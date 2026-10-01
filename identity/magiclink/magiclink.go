// Package magiclink composes purpose-bound email proof with AMOS's durable
// identity and session stores. The email challenge proves mailbox control at
// AAL1; it cannot waive a stronger organization policy.
package magiclink

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

const (
	MaxRequestBytes          = 4096
	MaxCookieHeaderBytes     = 8192
	MaxBrowserFlows          = 4
	MaxIssueRequestsPerHour  = 3
	DefaultChallengeLifetime = 30 * time.Minute
	challengePurpose         = store.ChallengeEmailMagicLink
	productionCookiePrefix   = "__Host-amos_magic_"
	developmentCookiePrefix  = "amos_dev_magic_"
)

var (
	ErrConfiguration               = errors.New("invalid magic-link configuration")
	ErrUnavailable                 = errors.New("magic-link dependency unavailable")
	ErrChallengeUnavailable        = errors.New("magic-link proof unavailable")
	ErrBrowserConfirmationRequired = errors.New("different-device confirmation required")
	ErrTooManyBrowserFlows         = errors.New("too many pending browser flows")
	ErrPolicyDenied                = errors.New("magic-link sign-in denied by policy")
	ErrStepUpRequired              = errors.New("magic-link sign-in requires stronger proof")
)

type SignInMaterialWriter interface {
	PutSignInMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error
}

type SessionIssuer interface {
	AllowsOrigin(*http.Request) bool
	IssueForRequestTx(context.Context, *sql.Tx, authproof.VerifiedCredential, *http.Request) (session.Issued, error)
}

// Policy is mandatory. It validates whether AAL1 email magic-link sign-in is
// permitted for the current person and organization memberships in tx. It must
// fail closed when policy state is missing or requires stronger assurance.
type Policy interface {
	// Ready verifies the policy dependency without looking up an account.
	// Implementations should check every required backing store/configuration.
	Ready(context.Context, *sql.Tx) error
	AuthorizeMagicLink(context.Context, *sql.Tx, uuid.UUID) error
}

type Config struct {
	DB                                           *storage.DB
	Outbox                                       *sqlstore.Store
	Renderer                                     *deliveryemail.Renderer
	Materials                                    SignInMaterialWriter
	Sessions                                     SessionIssuer
	Policy                                       Policy
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ApplicationOrigin                            string
	DevelopmentLoopback                          bool
	ChallengeLifetime                            time.Duration
}

type Service struct {
	cfg      Config
	origin   string
	cookieNS string
}

type issueRequest struct {
	Email string `json:"email"`
}

type confirmRequest struct {
	ChallengeID            string `json:"challenge_id"`
	Token                  string `json:"token"`
	ConfirmDifferentDevice bool   `json:"confirm_different_device"`
}

type response struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type previewData struct {
	Available    bool
	SameBrowser  bool
	ChallengeID  string
	Token        string
	ConfirmOther bool
	Nonce        string
}

func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Outbox == nil || cfg.Renderer == nil || cfg.Materials == nil || cfg.Sessions == nil || cfg.Policy == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrConfiguration
	}
	if cfg.ChallengeLifetime == 0 {
		cfg.ChallengeLifetime = DefaultChallengeLifetime
	}
	if cfg.ChallengeLifetime < deliveryemail.MinExpirySeconds*time.Second || cfg.ChallengeLifetime > deliveryemail.MaxExpirySeconds*time.Second || cfg.ChallengeLifetime > DefaultChallengeLifetime || cfg.ChallengeLifetime%time.Second != 0 {
		return nil, ErrConfiguration
	}
	origin, err := deliveryemail.ParseApplicationOrigin(cfg.ApplicationOrigin, cfg.DevelopmentLoopback)
	if err != nil {
		return nil, ErrConfiguration
	}
	probeID, err := store.NewID()
	if err != nil {
		return nil, ErrUnavailable
	}
	probeURL := actionURL(origin.String(), probeID, strings.Repeat("A", 43))
	probeRef := deliveryemail.SecretReference("material:" + probeID.String())
	if _, err := cfg.Renderer.Render(probeID, deliveryemail.Request{Template: deliveryemail.TemplateSignIn, MaterialRef: probeRef, ExpiresInSeconds: int64(cfg.ChallengeLifetime / time.Second)}, deliveryemail.PrivateMaterial{Recipient: "magic-link-probe@example.invalid", ActionURL: probeURL}); err != nil {
		return nil, ErrConfiguration
	}
	cookieNS := productionCookiePrefix
	if cfg.DevelopmentLoopback {
		cookieNS = developmentCookiePrefix
	}
	return &Service{cfg: cfg, origin: origin.String(), cookieNS: cookieNS}, nil
}

// RequestHandler handles POST /api/v1/identity/magic-links.
func (s *Service) RequestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.cfg.DB == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		if !s.cfg.Sessions.AllowsOrigin(r) {
			writeError(w, r, http.StatusForbidden, "request.origin_denied", "Request origin is unavailable.")
			return
		}
		var in issueRequest
		if !decodeJSON(w, r, &in) {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		address, valid := normalizeAddress(in.Email)
		if !valid {
			writeError(w, r, http.StatusBadRequest, "identity.magic_link_invalid", "Invalid sign-in request.")
			return
		}
		cookies, err := s.browserCookies(r)
		if err != nil {
			if errors.Is(err, ErrTooManyBrowserFlows) {
				writeError(w, r, http.StatusTooManyRequests, "identity.magic_link_limited", "Too many pending sign-in links.")
				return
			}
			writeError(w, r, http.StatusBadRequest, "identity.magic_link_invalid", "Invalid sign-in request.")
			return
		}
		if len(cookies) >= MaxBrowserFlows {
			writeError(w, r, http.StatusTooManyRequests, "identity.magic_link_limited", "Too many pending sign-in links.")
			return
		}
		if err := s.ready(r.Context()); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		challengeID, token, binding, requestID, err := newFlow()
		if err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if err := s.request(r.Context(), address, challengeID, token, binding, requestID); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		http.SetCookie(w, s.flowCookie(challengeID, binding, s.cfg.ChallengeLifetime))
		writeAccepted(w, r)
	})
}

// PreviewHandler handles GET/HEAD /magic-link without consuming proof or
// creating a session. A different browser must take an explicit confirm POST.
func (s *Service) PreviewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.cfg.DB == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		query := r.URL.Query()
		challengeValues, tokenValues := query["challenge"], query["token"]
		challengeValue, tokenValue := "", ""
		if len(challengeValues) == 1 && len(tokenValues) == 1 {
			challengeValue, tokenValue = challengeValues[0], tokenValues[0]
		}
		challengeID, idOK := parseID(challengeValue)
		token, digest, tokenErr := parseToken(tokenValue)
		data := previewData{}
		if idOK && tokenErr == nil {
			cookies, cookieErr := s.browserCookies(r)
			if cookieErr == nil {
				available, err := s.preview(r.Context(), challengeID, digest[:])
				if err != nil {
					writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
					return
				}
				if available {
					data.Available = true
					data.ChallengeID = challengeID.String()
					data.Token = token
					if cookieValue, ok := cookies[s.flowCookieName(challengeID)]; ok {
						_, bindingDigest, parseErr := parseToken(cookieValue)
						if parseErr == nil {
							data.SameBrowser = s.bindingMatches(r.Context(), challengeID, bindingDigest[:])
						}
					}
					data.ConfirmOther = !data.SameBrowser
				}
			}
		}
		writePreview(w, r.Method, data)
	})
}

// ConfirmHandler handles POST /magic-link/confirm. Challenge consumption and
// session rotation/insertion share one transaction; session cookies are set
// only after commit succeeds.
func (s *Service) ConfirmHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.cfg.DB == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		if !s.cfg.Sessions.AllowsOrigin(r) {
			writeError(w, r, http.StatusForbidden, "request.origin_denied", "Request origin is unavailable.")
			return
		}
		var in confirmRequest
		if !decodeJSON(w, r, &in) {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		challengeID, ok := parseID(in.ChallengeID)
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "identity.magic_link_unavailable", "Sign-in proof is unavailable.")
			return
		}
		_, digest, err := parseToken(in.Token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "identity.magic_link_unavailable", "Sign-in proof is unavailable.")
			return
		}
		cookies, err := s.browserCookies(r)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "identity.magic_link_unavailable", "Sign-in proof is unavailable.")
			return
		}
		bindingToken := cookies[s.flowCookieName(challengeID)]
		issued, err := s.confirm(r.Context(), challengeID, digest[:], bindingToken, in.ConfirmDifferentDevice, r)
		if err != nil {
			switch {
			case errors.Is(err, ErrChallengeUnavailable):
				writeError(w, r, http.StatusUnauthorized, "identity.magic_link_unavailable", "Sign-in proof is unavailable.")
			case errors.Is(err, ErrBrowserConfirmationRequired):
				writeError(w, r, http.StatusForbidden, "identity.magic_link_confirmation_required", "Confirm sign-in on this device to continue.")
			case errors.Is(err, ErrPolicyDenied), errors.Is(err, ErrStepUpRequired):
				writeError(w, r, http.StatusForbidden, "auth.forbidden", "Sign-in policy requirements were not met.")
			case errors.Is(err, ErrUnavailable):
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			default:
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			}
			return
		}
		http.SetCookie(w, issued.Cookie)
		http.SetCookie(w, s.expireFlowCookie(challengeID))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Authenticated bool   `json:"authenticated"`
			Assurance     string `json:"assurance"`
			CSRFToken     string `json:"csrf_token"`
		}{true, "aal1", issued.CSRFToken})
	})
}

func (s *Service) ready(ctx context.Context) error {
	var ready bool
	err := s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var readOnly, defaultReadOnly, recovery bool
		if err := tx.QueryRowContext(ctx, `SELECT current_setting('transaction_read_only')::boolean,
			current_setting('default_transaction_read_only')::boolean, pg_is_in_recovery()`).Scan(&readOnly, &defaultReadOnly, &recovery); err != nil {
			return err
		}
		if readOnly || defaultReadOnly || recovery {
			return ErrUnavailable
		}
		if err := tx.QueryRowContext(ctx, `SELECT
			COALESCE(has_table_privilege(current_user,to_regclass('identity_persons'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_emails'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'INSERT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_sessions'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_sessions'),'INSERT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_sessions'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('email_delivery_material'),'INSERT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('amos_jobs'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('amos_jobs'),'INSERT'),false)`).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return ErrUnavailable
		}
		return s.cfg.Policy.Ready(ctx, tx)
	})
	if err != nil || !ready {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) request(ctx context.Context, address string, challengeID uuid.UUID, token, binding string, requestID uuid.UUID) error {
	_, tokenDigest, err := parseToken(token)
	if err != nil {
		return ErrUnavailable
	}
	_, bindingDigest, err := parseToken(binding)
	if err != nil {
		return ErrUnavailable
	}
	err = s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var personID, emailID uuid.UUID
		var displayAddress string
		err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,e.display_address
			FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id
			WHERE p.installation_id=$1 AND p.application_id=$2 AND p.state='active'
			  AND e.installation_id=$1 AND e.application_id=$2 AND e.comparison_key=$3 AND e.verified_at IS NOT NULL
			FOR UPDATE OF p,e`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&personID, &emailID, &displayAddress)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.cfg.Policy.AuthorizeMagicLink(ctx, tx, personID); err != nil {
			if errors.Is(err, ErrPolicyDenied) || errors.Is(err, ErrStepUpRequired) {
				return nil
			}
			return err
		}
		var recent int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges
			WHERE person_id=$1 AND email_id=$2 AND purpose=$3
			  AND created_at >= transaction_timestamp() - interval '1 hour'`, personID, emailID, challengePurpose).Scan(&recent); err != nil {
			return err
		}
		if recent >= MaxIssueRequestsPerHour {
			return nil
		}
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp() + make_interval(secs => $1)`, s.cfg.ChallengeLifetime.Seconds()).Scan(&expires); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO identity_challenges
			(id,person_id,email_id,purpose,token_digest,browser_binding_digest,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, challengeID, personID, emailID, challengePurpose, tokenDigest[:], bindingDigest[:], expires)
		if err != nil {
			return err
		}
		ref := deliveryemail.SecretReference("material:" + challengeID.String())
		material := deliveryemail.PrivateMaterial{Recipient: displayAddress, ActionURL: actionURL(s.origin, challengeID, token)}
		if err := s.cfg.Materials.PutSignInMaterial(ctx, tx, ref, material, expires); err != nil {
			return err
		}
		request := deliveryemail.Request{Template: deliveryemail.TemplateSignIn, MaterialRef: ref, ExpiresInSeconds: int64(s.cfg.ChallengeLifetime / time.Second)}
		_, err = deliveryemail.EnqueueTx(ctx, tx, s.cfg.Outbox, s.cfg.Renderer, s.cfg.InstallationID, s.cfg.ApplicationID, "magic-link:"+requestID.String(), request, expires)
		return err
	})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) preview(ctx context.Context, challengeID uuid.UUID, digest []byte) (bool, error) {
	var available bool
	err := s.cfg.DB.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM identity_challenges c
			JOIN identity_persons p ON p.id=c.person_id
			JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
			WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3 AND c.consumed_at IS NULL
			  AND c.expires_at>transaction_timestamp() AND p.installation_id=$4 AND p.application_id=$5
			  AND p.state='active' AND e.installation_id=$4 AND e.application_id=$5 AND e.verified_at IS NOT NULL)`, challengeID, challengePurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&available)
	})
	if err != nil {
		return false, ErrUnavailable
	}
	return available, nil
}

func (s *Service) bindingMatches(ctx context.Context, challengeID uuid.UUID, digest []byte) bool {
	if len(digest) != sha256.Size {
		return false
	}
	var stored []byte
	err := s.cfg.DB.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT browser_binding_digest FROM identity_challenges
			WHERE id=$1 AND purpose=$2 AND consumed_at IS NULL AND expires_at>transaction_timestamp()`, challengeID, challengePurpose).Scan(&stored)
	})
	return err == nil && len(stored) == sha256.Size && subtle.ConstantTimeCompare(stored, digest) == 1
}

func (s *Service) confirm(ctx context.Context, challengeID uuid.UUID, digest []byte, binding string, confirmDifferentDevice bool, r *http.Request) (session.Issued, error) {
	var issued session.Issued
	if s == nil || s.cfg.DB == nil || ctx == nil || r == nil {
		return issued, ErrUnavailable
	}
	var tokenBindingDigest []byte
	if binding != "" {
		_, parsed, err := parseToken(binding)
		if err == nil {
			tokenBindingDigest = parsed[:]
		}
	}
	err := s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var personID, emailID, installationID, applicationID uuid.UUID
		var epoch int64
		var state string
		var verified sql.NullTime
		var storedBinding []byte
		err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,p.installation_id,p.application_id,p.security_epoch,p.state,e.verified_at,c.browser_binding_digest
			FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id
			JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
			WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3 AND c.consumed_at IS NULL
			  AND c.expires_at>transaction_timestamp() AND p.installation_id=$4 AND p.application_id=$5
			  AND p.state='active' AND e.installation_id=$4 AND e.application_id=$5 AND e.verified_at IS NOT NULL
			FOR UPDATE OF p,e,c`, challengeID, challengePurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&personID, &emailID, &installationID, &applicationID, &epoch, &state, &verified, &storedBinding)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrChallengeUnavailable
		}
		if err != nil {
			return err
		}
		if state != "active" || !verified.Valid || len(storedBinding) != sha256.Size {
			return ErrChallengeUnavailable
		}
		sameBrowser := len(tokenBindingDigest) == sha256.Size && subtle.ConstantTimeCompare(storedBinding, tokenBindingDigest) == 1
		if !sameBrowser && !confirmDifferentDevice {
			return ErrBrowserConfirmationRequired
		}
		if err := s.cfg.Policy.AuthorizeMagicLink(ctx, tx, personID); err != nil {
			return err
		}
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		consumed, err := identityStore.ConsumeChallenge(ctx, challengeID, challengePurpose, digest)
		if errors.Is(err, store.ErrChallengeUnavailable) {
			return ErrChallengeUnavailable
		}
		if err != nil {
			return err
		}
		if consumed.PersonID != personID || consumed.EmailID != emailID {
			return ErrChallengeUnavailable
		}
		var authenticatedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&authenticatedAt); err != nil {
			return err
		}
		proof, err := authproof.NewVerifiedCredential(personID, installationID, applicationID, s.cfg.EnvironmentID, epoch, "email_magic_link", authenticatedAt, "aal1", authenticatedAt.Add(12*time.Hour))
		if err != nil {
			return err
		}
		issued, err = s.cfg.Sessions.IssueForRequestTx(ctx, tx, proof, r)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrChallengeUnavailable) || errors.Is(err, ErrBrowserConfirmationRequired) || errors.Is(err, ErrPolicyDenied) || errors.Is(err, ErrStepUpRequired) {
			return session.Issued{}, err
		}
		return session.Issued{}, ErrUnavailable
	}
	return issued, nil
}

func (s *Service) flowCookieName(challengeID uuid.UUID) string {
	return s.cookieNS + challengeID.String()
}

func (s *Service) flowCookie(challengeID uuid.UUID, token string, lifetime time.Duration) *http.Cookie {
	cookie := &http.Cookie{Name: s.flowCookieName(challengeID), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(lifetime / time.Second)}
	if !s.cfg.DevelopmentLoopback {
		cookie.Secure = true
	}
	return cookie
}

func (s *Service) expireFlowCookie(challengeID uuid.UUID) *http.Cookie {
	cookie := &http.Cookie{Name: s.flowCookieName(challengeID), Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0).UTC()}
	if !s.cfg.DevelopmentLoopback {
		cookie.Secure = true
	}
	return cookie
}

func (s *Service) browserCookies(r *http.Request) (map[string]string, error) {
	if r == nil || len(r.Header.Values("Cookie")) > 1 {
		return nil, ErrChallengeUnavailable
	}
	if len(r.Header.Get("Cookie")) > MaxCookieHeaderBytes {
		return nil, ErrChallengeUnavailable
	}
	cookies := make(map[string]string, MaxBrowserFlows)
	otherPrefix := productionCookiePrefix
	if s.cookieNS == productionCookiePrefix {
		otherPrefix = developmentCookiePrefix
	}
	for _, cookie := range r.Cookies() {
		if strings.HasPrefix(cookie.Name, otherPrefix) {
			return nil, ErrChallengeUnavailable
		}
		if !strings.HasPrefix(cookie.Name, s.cookieNS) {
			continue
		}
		suffix := strings.TrimPrefix(cookie.Name, s.cookieNS)
		id, ok := parseID(suffix)
		if !ok || id.String() != suffix {
			return nil, ErrChallengeUnavailable
		}
		if _, exists := cookies[cookie.Name]; exists {
			return nil, ErrChallengeUnavailable
		}
		if _, _, err := parseToken(cookie.Value); err != nil {
			return nil, ErrChallengeUnavailable
		}
		cookies[cookie.Name] = cookie.Value
		if len(cookies) > MaxBrowserFlows {
			return nil, ErrTooManyBrowserFlows
		}
	}
	return cookies, nil
}

func normalizeAddress(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) > 254 || !utf8ASCII(value) {
		return "", false
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value {
		return "", false
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 64 || len(parts[1]) == 0 {
		return "", false
	}
	for _, ch := range value {
		if unicode.IsControl(ch) || strings.ContainsRune("<>() ,;:\\\"[]", ch) {
			return "", false
		}
	}
	return strings.ToLower(value), true
}

func utf8ASCII(value string) bool {
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func newFlow() (uuid.UUID, string, string, uuid.UUID, error) {
	challengeID, err1 := store.NewID()
	requestID, err2 := store.NewID()
	if err1 != nil || err2 != nil {
		return uuid.Nil, "", "", uuid.Nil, ErrUnavailable
	}
	token, _, err := newToken()
	if err != nil {
		return uuid.Nil, "", "", uuid.Nil, err
	}
	binding, _, err := newToken()
	if err != nil {
		return uuid.Nil, "", "", uuid.Nil, err
	}
	return challengeID, token, binding, requestID, nil
}

func newToken() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func parseToken(token string) (string, [32]byte, error) {
	if len(token) != 43 {
		return "", [32]byte{}, ErrChallengeUnavailable
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return "", [32]byte{}, ErrChallengeUnavailable
	}
	return token, sha256.Sum256([]byte(token)), nil
}

func parseID(value string) (uuid.UUID, bool) {
	id, err := uuid.Parse(value)
	return id, err == nil && validID(id) && id.String() == value
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func actionURL(origin string, challengeID uuid.UUID, token string) string {
	return origin + "/magic-link?challenge=" + challengeID.String() + "&token=" + url.QueryEscape(token)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r == nil || len(r.Header.Values("Content-Type")) != 1 || r.Body == nil || r.ContentLength > MaxRequestBytes {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func writeAccepted(w http.ResponseWriter, r *http.Request) {
	id, err := store.NewID()
	requestID := "unavailable"
	if err == nil {
		requestID = id.String()
	}
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(response{Code: "identity.magic_link.accepted", Message: "Request received. If the address belongs to an eligible account, sign-in instructions may be sent.", RequestID: requestID})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id, err := store.NewID()
	requestID := "unavailable"
	if err == nil {
		requestID = id.String()
	}
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Code: code, Message: message, RequestID: requestID})
}

var magicPreview = template.Must(template.New("magic-link").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>Email sign-in</title><body><main><h1>Email sign-in</h1>{{if .Available}}{{if .SameBrowser}}<p>Confirm to sign in to this browser.</p>{{else}}<p>This link was requested in another browser. Confirm to continue on this device.</p>{{end}}<form id="confirm" method="post" action="/magic-link/confirm"><input type="hidden" name="challenge_id" value="{{.ChallengeID}}"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="confirm_different_device" value="{{.ConfirmOther}}"><button type="submit">{{if .SameBrowser}}Continue sign-in{{else}}Confirm on this device{{end}}</button></form><p id="result" aria-live="polite"></p><script nonce="{{.Nonce}}">document.getElementById('confirm').addEventListener('submit',async function(event){event.preventDefault();const form=event.currentTarget;const values=new FormData(form);const body={challenge_id:values.get('challenge_id'),token:values.get('token'),confirm_different_device:values.get('confirm_different_device')==='true'};try{const response=await fetch('/magic-link/confirm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(response.ok){const result=await response.json();document.getElementById('result').textContent='Signed in. You may continue.';}else{document.getElementById('result').textContent='This sign-in link is unavailable or expired.';}form.hidden=true;}catch(_){document.getElementById('result').textContent='Sign-in is temporarily unavailable.';}});</script>{{else}}<p>This sign-in link is invalid, expired, already used, or unavailable.</p>{{end}}</main></body></html>`))

func writePreview(w http.ResponseWriter, method string, data previewData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if data.Available {
		var nonce [18]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		data.Nonce = base64.RawURLEncoding.EncodeToString(nonce[:])
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+data.Nonce+"'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	}
	w.WriteHeader(http.StatusOK)
	if method != http.MethodHead {
		_ = magicPreview.Execute(w, data)
	}
}
