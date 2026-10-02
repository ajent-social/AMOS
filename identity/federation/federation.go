// Package federation binds verified provider identities to local people through
// browser-bound, one-use authorization callbacks.
package federation

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	flowLifetime = 10 * time.Minute
	proofAge     = 15 * time.Minute
)

var (
	ErrInvalidInput        = errors.New("invalid federation input")
	ErrUnavailable         = errors.New("federation unavailable")
	ErrCallbackRejected    = errors.New("federation callback rejected")
	ErrIdentityUnavailable = errors.New("federated identity unavailable")
	ErrIdentityConflict    = errors.New("federated identity already linked")
)

type Intent string

const (
	IntentLogin Intent = "login"
	IntentLink  Intent = "link"
)

// Provider constructs a provider authorization URL without network calls,
// then exchanges a one-use authorization code and returns only claims it has
// verified. OIDC implementations also validate expectedNonce.
type Provider interface {
	AuthorizationURL(ctx context.Context, flow Authorization) (string, error)
	Exchange(ctx context.Context, code, verifier, expectedNonce string) (Identity, error)
}

// Identity contains the provider's verified stable identity key. Email is
// profile data and is never consulted for account resolution or linking.
type Identity struct {
	Issuer  string
	Subject string
	Nonce   string
	Email   string
}

type Connection struct {
	Provider             string
	ProviderConnectionID uuid.UUID
	Issuer               string
}

type Config struct {
	DB             *storage.DB
	Sessions       *session.Service
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
	SecureCookies  bool
	CallbackURL    string
	Providers      map[string]Provider
	Connections    map[string]Connection
}

type Service struct {
	db       *storage.DB
	sessions *session.Service
	cfg      Config
	cookie   string
}

type BeginRequest struct {
	Connection string
	ReturnTo   string
}

type Authorization struct {
	State                string
	Nonce                string
	CodeChallenge        string
	CodeChallengeMethod  string
	CallbackURL          string
	ProviderURL          string
	ReturnTo             string
	ProviderConnectionID uuid.UUID
}

func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Sessions == nil || !validID(cfg.InstallationID) ||
		!validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) ||
		len(cfg.Providers) == 0 || len(cfg.Connections) == 0 || !validCallbackURL(cfg.CallbackURL, cfg.SecureCookies) {
		return nil, ErrInvalidInput
	}
	for name, provider := range cfg.Providers {
		if !validProviderMethod(name) || provider == nil {
			return nil, ErrInvalidInput
		}
	}
	for name, connection := range cfg.Connections {
		if !validLabel(name) || cfg.Providers[connection.Provider] == nil ||
			!validID(connection.ProviderConnectionID) || !validIssuer(connection.Issuer) {
			return nil, ErrInvalidInput
		}
	}
	providers := make(map[string]Provider, len(cfg.Providers))
	for key, value := range cfg.Providers {
		providers[key] = value
	}
	connections := make(map[string]Connection, len(cfg.Connections))
	for key, value := range cfg.Connections {
		connections[key] = value
	}
	cfg.Providers, cfg.Connections = providers, connections
	cookie := "amos_dev_federation"
	if cfg.SecureCookies {
		cookie = "__Host-amos_federation"
	}
	return &Service{db: cfg.DB, sessions: cfg.Sessions, cfg: cfg, cookie: cookie}, nil
}

func (s *Service) BeginLogin(w http.ResponseWriter, r *http.Request, input BeginRequest) (Authorization, error) {
	return s.begin(w, r, input, IntentLogin, identity.Principal{}, nil)
}

// BeginLink requires a recent principal from authenticated middleware. It does
// not accept a caller-selected person ID or infer ownership from email.
func (s *Service) BeginLink(w http.ResponseWriter, r *http.Request, input BeginRequest) (Authorization, error) {
	if s == nil || r == nil {
		return Authorization{}, ErrInvalidInput
	}
	if r.Method != http.MethodPost || !s.sessions.AllowsOrigin(r) {
		return Authorization{}, ErrCallbackRejected
	}
	sessionDigest, ok := currentSessionDigest(r, s.sessions, s.cfg.SecureCookies)
	if !ok {
		return Authorization{}, ErrCallbackRejected
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	now := time.Now()
	if !ok || principal.InstallationID() != s.cfg.InstallationID ||
		principal.ApplicationID() != s.cfg.ApplicationID ||
		principal.EnvironmentID() != s.cfg.EnvironmentID ||
		now.Before(principal.AuthenticatedAt()) || now.Sub(principal.AuthenticatedAt()) > proofAge ||
		principal.Assurance().ExpiresAt().Before(now) {
		return Authorization{}, ErrCallbackRejected
	}
	return s.begin(w, r, input, IntentLink, principal, sessionDigest)
}

func (s *Service) begin(w http.ResponseWriter, r *http.Request, input BeginRequest, intent Intent, principal identity.Principal, sessionDigest []byte) (Authorization, error) {
	if s == nil || s.db == nil || w == nil || r == nil {
		return Authorization{}, ErrInvalidInput
	}
	connection, ok := s.cfg.Connections[input.Connection]
	if !ok {
		return Authorization{}, ErrInvalidInput
	}
	returnTo, err := safeReturnTarget(input.ReturnTo)
	if err != nil {
		return Authorization{}, err
	}
	state, err := randomToken()
	if err != nil {
		return Authorization{}, ErrUnavailable
	}
	browserToken := ""
	if cookie, cookieErr := r.Cookie(s.cookie); cookieErr == nil && validRandomToken(cookie.Value) {
		browserToken = cookie.Value
	} else {
		browserToken, err = randomToken()
		if err != nil {
			return Authorization{}, ErrUnavailable
		}
	}
	verifier := derive("amos-federation-pkce-v1:", browserToken, state)
	nonce := derive("amos-federation-nonce-v1:", browserToken, state)
	stateHash, browserHash, nonceHash := digest(state), digest(browserToken), digest(nonce)
	flowID, err := uuid.NewV7()
	if err != nil {
		return Authorization{}, ErrUnavailable
	}
	var person any
	var authenticatedAt any
	var securityEpoch any
	var assuranceLevel any
	var assuranceExpires any
	var boundSession any
	if intent == IntentLink {
		person = principal.PersonID()
		authenticatedAt = principal.AuthenticatedAt()
		securityEpoch = principal.SecurityEpoch()
		assuranceLevel = string(principal.Assurance().Level())
		assuranceExpires = principal.Assurance().ExpiresAt()
		boundSession = sessionDigest
	} else {
		securityEpoch, assuranceLevel, assuranceExpires = nil, nil, nil
		boundSession = nil
	}
	authorization := Authorization{State: state, Nonce: nonce,
		CodeChallenge: digestString(verifier), CodeChallengeMethod: "S256",
		CallbackURL: s.cfg.CallbackURL + "?provider=" + url.QueryEscape(connection.Provider),
		ReturnTo:    returnTo, ProviderConnectionID: connection.ProviderConnectionID}
	providerURL, err := s.cfg.Providers[connection.Provider].AuthorizationURL(r.Context(), authorization)
	if err != nil || !validAuthorizationURL(providerURL, authorization) {
		return Authorization{}, ErrUnavailable
	}
	authorization.ProviderURL = providerURL
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := checkConnection(ctx, tx, s, connection); err != nil {
			return err
		}
		_, execErr := tx.ExecContext(ctx, `INSERT INTO identity_federation_flows
			(id,state_digest,browser_digest,nonce_digest,provider,provider_connection_id,
			 installation_id,application_id,environment_id,issuer,intent,person_id,
			 authenticated_at,security_epoch,assurance_level,assurance_expires_at,session_digest,return_to,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
			 clock_timestamp()+interval '10 minutes')`,
			flowID, stateHash[:], browserHash[:], nonceHash[:], connection.Provider,
			connection.ProviderConnectionID, s.cfg.InstallationID, s.cfg.ApplicationID,
			s.cfg.EnvironmentID, connection.Issuer, string(intent), person,
			authenticatedAt, securityEpoch, assuranceLevel, assuranceExpires, boundSession, returnTo)
		return execErr
	})
	if err != nil {
		return Authorization{}, ErrUnavailable
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookie, Value: browserToken, Path: "/", HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(flowLifetime.Seconds())})
	return authorization, nil
}

// Callback commits flow consumption and identity/session authority atomically.
// The session cookie is emitted only after the database transaction commits.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.db == nil || w == nil || r == nil {
		writeStatus(w, http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		writeStatus(w, http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	state, code := query.Get("state"), query.Get("code")
	providerName := query.Get("provider")
	if len(query["state"]) != 1 || len(query["code"]) != 1 || len(query["provider"]) != 1 {
		writeStatus(w, http.StatusBadRequest)
		return
	}
	if !validRandomToken(state) || code == "" || len(code) > 4096 || !validProviderMethod(providerName) {
		writeStatus(w, http.StatusBadRequest)
		return
	}
	cookie, cookieCount := oneCookie(r, s.cookie)
	if cookieCount != 1 || !validRandomToken(cookie.Value) {
		writeStatus(w, http.StatusUnauthorized)
		return
	}
	stateHash, browserHash := digest(state), digest(cookie.Value)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var flow pendingFlow
	err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT f.provider,f.provider_connection_id,f.issuer,f.intent,
			f.person_id,f.authenticated_at,f.nonce_digest,f.return_to,f.installation_id,
			f.application_id,f.environment_id,f.security_epoch,f.assurance_level,f.assurance_expires_at,f.session_digest
			FROM identity_federation_flows f JOIN identity_federation_connections c
			ON c.id=f.provider_connection_id AND c.enabled AND c.provider=f.provider AND c.issuer=f.issuer
			AND c.installation_id=f.installation_id AND c.application_id=f.application_id
			WHERE f.state_digest=$1 AND f.browser_digest=$2 AND f.provider=$3
			AND f.consumed_at IS NULL AND f.expires_at>clock_timestamp()`,
			stateHash[:], browserHash[:], providerName).Scan(&flow.provider,
			&flow.connectionID, &flow.issuer, &flow.intent, &flow.personID,
			&flow.authenticatedAt, &flow.nonceDigest, &flow.returnTo, &flow.installationID,
			&flow.applicationID, &flow.environmentID, &flow.securityEpoch,
			&flow.assuranceLevel, &flow.assuranceExpires, &flow.sessionDigest)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeStatus(w, http.StatusUnauthorized)
		return
	}
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable)
		return
	}
	flow.stateDigest = stateHash[:]
	if flow.intent == string(IntentLink) {
		sessionCookie, sessionCount := oneCookie(r, sessionCookieName(s.cfg.SecureCookies))
		cookieHash := digest(sessionCookie.Value)
		if sessionCount != 1 || !validRandomToken(sessionCookie.Value) || !constantBytesEqual(flow.sessionDigest, cookieHash[:]) {
			writeStatus(w, http.StatusUnauthorized)
			return
		}
	}
	provider := s.cfg.Providers[flow.provider]
	if provider == nil {
		writeStatus(w, http.StatusServiceUnavailable)
		return
	}
	verifier := derive("amos-federation-pkce-v1:", cookie.Value, state)
	nonce := derive("amos-federation-nonce-v1:", cookie.Value, state)
	verified, err := provider.Exchange(ctx, code, verifier, nonce)
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable)
		return
	}
	if !validIssuer(verified.Issuer) || !validToken(verified.Subject) ||
		verified.Issuer != flow.issuer || !constantDigestEqual(flow.nonceDigest, verified.Nonce) {
		writeStatus(w, http.StatusUnauthorized)
		return
	}
	var issued session.Issued
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var locked pendingFlow
		lockErr := tx.QueryRowContext(ctx, `SELECT f.provider_connection_id,f.issuer,f.intent,
			f.person_id,f.authenticated_at,f.return_to,f.installation_id,f.application_id,
			f.environment_id,f.security_epoch,f.assurance_level,f.assurance_expires_at,f.session_digest
			FROM identity_federation_flows f JOIN identity_federation_connections c
			ON c.id=f.provider_connection_id AND c.enabled AND c.provider=f.provider AND c.issuer=f.issuer
			AND c.installation_id=f.installation_id AND c.application_id=f.application_id
			WHERE f.state_digest=$1 AND f.browser_digest=$2 AND f.provider=$3
			AND f.consumed_at IS NULL AND f.expires_at>clock_timestamp()
			AND (f.intent='login' OR (f.authenticated_at>clock_timestamp()-interval '15 minutes'
			AND f.authenticated_at<=clock_timestamp() AND f.assurance_expires_at>clock_timestamp()))
			FOR UPDATE OF f FOR SHARE OF c`, stateHash[:], browserHash[:], providerName).Scan(&locked.connectionID,
			&locked.issuer, &locked.intent, &locked.personID, &locked.authenticatedAt, &locked.returnTo,
			&locked.installationID, &locked.applicationID, &locked.environmentID, &locked.securityEpoch,
			&locked.assuranceLevel, &locked.assuranceExpires, &locked.sessionDigest)
		if lockErr != nil || locked.connectionID != flow.connectionID || locked.issuer != flow.issuer || locked.intent != flow.intent {
			return ErrCallbackRejected
		}
		switch flow.intent {
		case string(IntentLink):
			if !flow.personID.Valid || !flow.authenticatedAt.Valid || !sameFlowAuthority(flow, locked) {
				return ErrCallbackRejected
			}
			if err := insertBinding(ctx, tx, s, flow, verified); err != nil {
				return err
			}
		case string(IntentLogin):
			proof, proofErr := findLoginProof(ctx, tx, s, flow, verified)
			if proofErr != nil {
				return proofErr
			}
			issued, proofErr = s.sessions.IssueForRequestTx(ctx, tx, proof, r)
			if proofErr != nil {
				return proofErr
			}
		default:
			return ErrCallbackRejected
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE identity_federation_flows f SET consumed_at=clock_timestamp()
			WHERE f.state_digest=$1 AND f.browser_digest=$2 AND f.provider=$3 AND f.consumed_at IS NULL
			AND f.expires_at>clock_timestamp() AND EXISTS (SELECT 1 FROM identity_federation_connections c
			WHERE c.id=f.provider_connection_id AND c.enabled AND c.installation_id=f.installation_id
			AND c.application_id=f.application_id AND c.provider=f.provider AND c.issuer=f.issuer)
			AND (f.intent='login' OR (f.authenticated_at>clock_timestamp()-interval '15 minutes'
			AND f.authenticated_at<=clock_timestamp() AND f.assurance_expires_at>clock_timestamp()))
			AND (f.intent='login' OR EXISTS (SELECT 1 FROM identity_sessions s JOIN identity_persons p ON p.id=s.person_id
			WHERE s.token_digest=f.session_digest AND s.person_id=f.person_id AND s.installation_id=f.installation_id
			AND s.application_id=f.application_id AND s.environment_id=f.environment_id
			AND s.security_epoch=f.security_epoch AND s.revoked_at IS NULL
			AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
			AND p.state='active' AND p.security_epoch=f.security_epoch))`,
			stateHash[:], browserHash[:], providerName)
		if updateErr != nil {
			return updateErr
		}
		count, countErr := result.RowsAffected()
		if countErr != nil || count != 1 {
			return ErrCallbackRejected
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrCallbackRejected) || errors.Is(err, ErrIdentityUnavailable) || errors.Is(err, ErrIdentityConflict) {
			writeStatus(w, http.StatusUnauthorized)
		} else {
			writeStatus(w, http.StatusServiceUnavailable)
		}
		return
	}
	if flow.intent == string(IntentLogin) {
		http.SetCookie(w, issued.Cookie)
	}
	http.Redirect(w, r, flow.returnTo, http.StatusSeeOther)
}

type pendingFlow struct {
	provider         string
	connectionID     uuid.UUID
	issuer           string
	intent           string
	personID         uuid.NullUUID
	authenticatedAt  sql.NullTime
	nonceDigest      []byte
	returnTo         string
	installationID   uuid.UUID
	applicationID    uuid.UUID
	environmentID    uuid.UUID
	securityEpoch    sql.NullInt64
	assuranceLevel   sql.NullString
	assuranceExpires sql.NullTime
	stateDigest      []byte
	sessionDigest    []byte
}

func insertBinding(ctx context.Context, tx *sql.Tx, s *Service, flow pendingFlow, verified Identity) error {
	if !flow.personID.Valid {
		return ErrCallbackRejected
	}
	var state string
	var epoch int64
	var fresh bool
	err := tx.QueryRowContext(ctx, `SELECT p.state,p.security_epoch,
		f.authenticated_at>clock_timestamp()-interval '15 minutes' AND f.authenticated_at<=clock_timestamp()
		AND f.assurance_expires_at>clock_timestamp()
		FROM identity_persons p JOIN identity_federation_flows f ON f.person_id=p.id
		JOIN identity_sessions s ON s.token_digest=f.session_digest AND s.person_id=p.id
		AND s.installation_id=f.installation_id AND s.application_id=f.application_id
		AND s.environment_id=f.environment_id AND s.security_epoch=f.security_epoch
		AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
		WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3
		AND p.state='active' AND p.security_epoch=f.security_epoch AND f.session_digest=$5
		AND f.state_digest=$4 FOR UPDATE OF p,s`, flow.personID.UUID,
		s.cfg.InstallationID, s.cfg.ApplicationID, flow.stateDigest, flow.sessionDigest).Scan(&state, &epoch, &fresh)
	if err != nil || state != "active" || !flow.securityEpoch.Valid || epoch != flow.securityEpoch.Int64 || !fresh ||
		flow.installationID != s.cfg.InstallationID || flow.applicationID != s.cfg.ApplicationID ||
		flow.environmentID != s.cfg.EnvironmentID || flow.assuranceLevel.String == "" {
		return ErrIdentityUnavailable
	}
	bindingID, err := uuid.NewV7()
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO identity_external_bindings
		(id,person_id,provider_connection_id,provider,issuer,subject)
		VALUES ($1,$2,$3,$4,$5,$6)`, bindingID, flow.personID.UUID,
		flow.connectionID, flow.provider, flow.issuer, verified.Subject)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrIdentityConflict
		}
		return err
	}
	return nil
}

func findLoginProof(ctx context.Context, tx *sql.Tx, s *Service, flow pendingFlow, verified Identity) (authproof.VerifiedCredential, error) {
	var personID, installationID, applicationID uuid.UUID
	var epoch int64
	var state string
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.installation_id,p.application_id,p.security_epoch,p.state
		FROM identity_external_bindings b JOIN identity_persons p ON p.id=b.person_id
		WHERE b.provider_connection_id=$1 AND b.provider=$2 AND b.issuer=$3 AND b.subject=$4
		AND p.installation_id=$5 AND p.application_id=$6 FOR UPDATE OF p`,
		flow.connectionID, flow.provider, flow.issuer, verified.Subject,
		s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&personID, &installationID, &applicationID, &epoch, &state)
	if err != nil || state != "active" || epoch < 0 {
		return authproof.VerifiedCredential{}, ErrIdentityUnavailable
	}
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(personID, installationID, applicationID,
		s.cfg.EnvironmentID, epoch, flow.provider, now, "aal1", now.Add(12*time.Hour))
	if err != nil {
		return authproof.VerifiedCredential{}, ErrIdentityUnavailable
	}
	return proof, nil
}

func checkConnection(ctx context.Context, tx *sql.Tx, s *Service, configured Connection) error {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT enabled FROM identity_federation_connections
		WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND provider=$4 AND issuer=$5
		FOR SHARE`, configured.ProviderConnectionID, s.cfg.InstallationID, s.cfg.ApplicationID,
		configured.Provider, configured.Issuer).Scan(&enabled)
	if err != nil || !enabled {
		return ErrIdentityUnavailable
	}
	return nil
}

func sameFlowAuthority(a, b pendingFlow) bool {
	return a.installationID == b.installationID && a.applicationID == b.applicationID &&
		a.environmentID == b.environmentID && a.personID == b.personID &&
		a.securityEpoch == b.securityEpoch && a.authenticatedAt == b.authenticatedAt &&
		a.assuranceLevel == b.assuranceLevel && a.assuranceExpires == b.assuranceExpires &&
		hmac.Equal(a.sessionDigest, b.sessionDigest)
}

func currentSessionDigest(r *http.Request, sessions *session.Service, secure bool) ([]byte, bool) {
	if len(r.Header.Values("X-CSRF-Token")) != 1 {
		return nil, false
	}
	cookieName := "amos_dev_session"
	if secure {
		cookieName = "__Host-amos_session"
	}
	cookie, count := oneCookie(r, cookieName)
	if count != 1 || !validRandomToken(cookie.Value) {
		return nil, false
	}
	expected, ok := sessions.CSRFToken(r)
	if !ok || !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-CSRF-Token"))) {
		return nil, false
	}
	sum := digest(cookie.Value)
	return sum[:], true
}

func oneCookie(r *http.Request, name string) (*http.Cookie, int) {
	var result *http.Cookie
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			count++
			result = cookie
		}
	}
	return result, count
}

func sessionCookieName(secure bool) string {
	if secure {
		return "__Host-amos_session"
	}
	return "amos_dev_session"
}

func validCallbackURL(value string, secure bool) bool {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "/oauth/callback" ||
		u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if secure {
		return u.Scheme == "https"
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

func validAuthorizationURL(value string, flow Authorization) bool {
	u, err := url.Parse(value)
	if err != nil || !u.IsAbs() || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	query := u.Query()
	return len(query["state"]) == 1 && query.Get("state") == flow.State &&
		len(query["nonce"]) == 1 && query.Get("nonce") == flow.Nonce &&
		len(query["redirect_uri"]) == 1 && query.Get("redirect_uri") == flow.CallbackURL &&
		len(query["code_challenge"]) == 1 && query.Get("code_challenge") == flow.CodeChallenge &&
		len(query["code_challenge_method"]) == 1 && query.Get("code_challenge_method") == "S256"
}

func safeReturnTarget(value string) (string, error) {
	if value == "" {
		return "/", nil
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(u.Path, "//") || u.User != nil || u.Fragment != "" ||
		strings.ContainsAny(value, "\\\r\n\x00") {
		return "", ErrInvalidInput
	}
	return u.RequestURI(), nil
}

func derive(prefix, browserToken, state string) string {
	mac := hmac.New(sha256.New, []byte(browserToken))
	_, _ = mac.Write([]byte(prefix + state))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func digest(value string) [32]byte { return sha256.Sum256([]byte(value)) }
func digestString(value string) string {
	sum := digest(value)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func constantDigestEqual(expected []byte, actual string) bool {
	if len(expected) != sha256.Size || actual == "" {
		return false
	}
	sum := digest(actual)
	return hmac.Equal(expected, sum[:])
}
func constantBytesEqual(a, b []byte) bool { return len(a) == len(b) && hmac.Equal(a, b) }
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate callback token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func validRandomToken(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validProviderMethod(value string) bool {
	return value == "google" || value == "github" || value == "apple" || value == "enterprise_oidc"
}
func validLabel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
func validToken(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}

func validIssuer(value string) bool {
	u, err := url.Parse(value)
	return err == nil && validToken(value) && u.Scheme == "https" && u.Host != "" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func writeStatus(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
}
