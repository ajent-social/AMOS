// Package login implements public registration and email-password sign-in.
package login

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"reflect"
	"strings"
	"time"

	"github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/ajent-social/amos/workspace/personal"
	"github.com/google/uuid"
)

const (
	MaxRequestBytes          = 4096
	DefaultChallengeLifetime = 30 * time.Minute
)

var ErrConfiguration = errors.New("invalid login configuration")

type Config struct {
	DB                                           *storage.DB
	Passwords                                    *password.Hasher
	Email                                        *email.Service
	Sessions                                     *session.Service
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ChallengeLifetime                            time.Duration
}

// TxRunner is the transaction capability required by a login service.
// The caller owns its lifetime and binds all participants to the same database.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type TxConfig struct {
	Passwords                                    *password.Hasher
	Email                                        *email.Service
	Sessions                                     *session.Service
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ChallengeLifetime                            time.Duration
}

type Service struct {
	db   TxRunner
	cfg  TxConfig
	root *aw.Root
}

func New(cfg Config) (*Service, error) {
	return NewWithTxRunner(cfg.DB, TxConfig{
		Passwords:         cfg.Passwords,
		Email:             cfg.Email,
		Sessions:          cfg.Sessions,
		InstallationID:    cfg.InstallationID,
		ApplicationID:     cfg.ApplicationID,
		EnvironmentID:     cfg.EnvironmentID,
		ChallengeLifetime: cfg.ChallengeLifetime,
	})
}

// NewWithTxRunner constructs a service without probing or taking ownership of db.
func NewWithTxRunner(db TxRunner, cfg TxConfig) (*Service, error) {
	if aw.SelectLegacy() != nil || nilTxRunner(db) || cfg.Passwords == nil || cfg.Email == nil || cfg.Sessions == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrConfiguration
	}
	if cfg.ChallengeLifetime == 0 {
		cfg.ChallengeLifetime = DefaultChallengeLifetime
	}
	if cfg.ChallengeLifetime != email.DefaultChallengeLifetime {
		return nil, ErrConfiguration
	}
	return &Service{db: db, cfg: cfg}, nil
}

func nilTxRunner(db TxRunner) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Handler serves POST /signup and POST /auth. Mount these routes explicitly.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var h http.HandlerFunc
		switch r.URL.Path {
		case "/signup":
			h = s.register
		case "/auth":
			h = s.signIn
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			write(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		if !s.cfg.Sessions.AllowsOrigin(r) {
			write(w, r, http.StatusForbidden, "auth.forbidden", "Request origin is unavailable.")
			return
		}
		h(w, r)
	})
}

type registrationRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type response struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	if s != nil && s.root != nil {
		s.registerWriter(w, r)
		return
	}
	if aw.SelectLegacy() != nil {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	var in registrationRequest
	if !decode(w, r, &in) {
		write(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
		return
	}
	address, ok := normalizeAddress(in.Email)
	if !ok {
		write(w, r, http.StatusBadRequest, "identity.registration_invalid", "Invalid registration details.")
		return
	}
	hash, err := s.cfg.Passwords.Hash(r.Context(), "register:"+address, in.Password)
	if err != nil {
		if errors.Is(err, password.ErrInvalid) {
			write(w, r, http.StatusBadRequest, "identity.registration_invalid", "Invalid registration details.")
		} else {
			write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		}
		return
	}
	personID, e1 := store.NewID()
	emailID, e2 := store.NewID()
	credentialID, e3 := store.NewID()
	challengeID, e4 := store.NewID()
	workspaceID, e5 := store.NewID()
	requestID, e6 := store.NewID()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	var expiry time.Time
	err = s.db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(r.Context(), `SELECT transaction_timestamp() + make_interval(secs => $1)`, s.cfg.ChallengeLifetime.Seconds()).Scan(&expiry); err != nil {
			return err
		}
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		pending, err := st.CreatePendingRegistration(r.Context(), store.PendingAccount{PersonID: personID, EmailID: emailID, CredentialID: credentialID, ChallengeID: challengeID, InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EmailAddress: address, PasswordHash: hash, ChallengeDigest: digest[:], ChallengeExpiry: expiry})
		if err != nil {
			return err
		}
		p, err := personal.New(tx)
		if err != nil {
			return err
		}
		if _, err = p.BootstrapPending(r.Context(), pending, workspaceID); err != nil {
			return err
		}
		return s.cfg.Email.QueueExistingChallengeTx(r.Context(), tx, personID, emailID, challengeID, requestID, token)
	})
	// Natural-idempotent registration deliberately returns the same accepted
	// response for a duplicate scoped email, without creating a session.
	if err != nil && !errors.Is(err, store.ErrEmailAlreadyUsed) {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	write(w, r, http.StatusAccepted, "identity.registration_accepted", "If the address can be registered, a verification message will be sent.")
}

func (s *Service) signIn(w http.ResponseWriter, r *http.Request) {
	if s != nil && s.root != nil {
		s.signInWriter(w, r)
		return
	}
	if aw.SelectLegacy() != nil {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	var in signInRequest
	if !decode(w, r, &in) {
		write(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
		return
	}
	address, valid := normalizeAddress(in.Email)
	if !valid {
		address = "invalid@example.invalid"
	}
	var personID, installationID, applicationID, emailID, credentialID uuid.UUID
	var comparisonKey string
	environmentID := s.cfg.EnvironmentID
	var epoch int64
	var state string
	var verified sql.NullTime
	var encoded string
	err := s.db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(r.Context(), `SELECT p.id,p.installation_id,p.application_id,p.security_epoch,p.state,e.verified_at,c.verifier_hash,e.id,e.comparison_key,c.id
			FROM identity_emails e JOIN identity_persons p ON p.id=e.person_id AND p.installation_id=e.installation_id AND p.application_id=e.application_id
			JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
			WHERE e.installation_id=$1 AND e.application_id=$2 AND e.comparison_key=lower($3)
			LIMIT 1`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&personID, &installationID, &applicationID, &epoch, &state, &verified, &encoded, &emailID, &comparisonKey, &credentialID)
	})
	known := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	expectedHash := encoded
	var persist password.PersistRehash
	if known {
		persist = func(ctx context.Context, oldHash, newHash string) error {
			err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				result, err := tx.ExecContext(ctx, `UPDATE identity_credentials SET verifier_hash=$1
					WHERE person_id=$2 AND method='email_password' AND verifier_hash=$3 AND revoked_at IS NULL
					AND id=$6 AND EXISTS (SELECT 1 FROM identity_persons p WHERE p.id=$2 AND p.installation_id=$4 AND p.application_id=$5)`, newHash, personID, oldHash, s.cfg.InstallationID, s.cfg.ApplicationID, credentialID)
				if err != nil {
					return err
				}
				updated, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if updated != 1 {
					return sql.ErrNoRows
				}
				return nil
			})
			if err == nil {
				expectedHash = newHash
			}
			return err
		}
	}
	result, hashErr := s.cfg.Passwords.Verify(r.Context(), "signin:"+address, in.Password, encoded, persist)
	if hashErr != nil && !errors.Is(hashErr, password.ErrInvalid) {
		write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		return
	}
	if !valid || !known || !result.Verified || state != "active" || !verified.Valid {
		write(w, r, http.StatusUnauthorized, "auth.unauthenticated", "Email or password is incorrect.")
		return
	}
	if !result.RehashPersisted {
		expectedHash = encoded
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	stale := errors.New("stale password admission")
	var issued session.Issued
	err = s.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: false}, func(tx *sql.Tx) error {
		var isolation, readOnly string
		if err := tx.QueryRowContext(ctx, `SELECT pg_catalog.current_setting('transaction_isolation'),pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil {
			return err
		}
		if isolation != "read committed" || readOnly != "off" {
			return storage.ErrTransaction
		}
		var currentState string
		var currentEpoch int64
		if err := tx.QueryRowContext(ctx, `SELECT state,security_epoch FROM identity_persons
			WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE`, personID, installationID, applicationID).Scan(&currentState, &currentEpoch); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return stale
			}
			return err
		}
		if currentState != "active" || currentEpoch != epoch {
			return stale
		}
		var currentKey string
		var currentVerified sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT comparison_key,verified_at FROM identity_emails
			WHERE id=$1 AND person_id=$2 AND installation_id=$3 AND application_id=$4 FOR SHARE`, emailID, personID, installationID, applicationID).Scan(&currentKey, &currentVerified); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return stale
			}
			return err
		}
		if currentKey != comparisonKey || !currentVerified.Valid || !currentVerified.Time.Equal(verified.Time) {
			return stale
		}
		var currentMethod, currentHash string
		var revoked sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT method,verifier_hash,revoked_at FROM identity_credentials
			WHERE id=$1 AND person_id=$2 FOR SHARE`, credentialID, personID).Scan(&currentMethod, &currentHash, &revoked); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return stale
			}
			return err
		}
		if currentMethod != "email_password" || revoked.Valid || currentHash != expectedHash {
			return stale
		}
		var now time.Time
		if err := tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		proof, err := authproof.NewVerifiedCredential(personID, installationID, applicationID, environmentID, epoch, "email_password", now, "aal1", now.Add(12*time.Hour))
		if err != nil {
			return err
		}
		issued, err = s.cfg.Sessions.IssueForRequestTx(ctx, tx, proof, r)
		return err
	})
	if err != nil {
		if errors.Is(err, stale) && !errors.Is(err, storage.ErrTransaction) && ctx.Err() == nil {
			write(w, r, http.StatusUnauthorized, "auth.unauthenticated", "Email or password is incorrect.")
		} else {
			write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
		}
		return
	}
	http.SetCookie(w, issued.Cookie)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{true, issued.CSRFToken})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" || len(r.Header.Values("Content-Type")) != 1 || r.Body == nil || r.ContentLength > MaxRequestBytes {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil {
		return false
	}
	var extra any
	return dec.Decode(&extra) == io.EOF
}

func normalizeAddress(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 {
		return "", false
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || len(parts[0]) > 64 {
		return "", false
	}
	for _, ch := range value {
		if ch < 0x21 || ch > 0x7e || strings.ContainsRune("<>() ,;:\\\"[]", ch) {
			return "", false
		}
	}
	return strings.ToLower(value), true
}
func write(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id := w.Header().Get("X-Request-ID")
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id || !validID(parsed) {
		generated, err := store.NewID()
		if err != nil {
			id = "unavailable"
		} else {
			id = generated.String()
		}
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Code: code, Message: message, RequestID: id})
}
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
