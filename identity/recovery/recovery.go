// Package recovery implements purpose-bound password reset and authenticated
// password change. Delivery and identity mutations commit in one SQL transaction.
package recovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"reflect"
	"strings"
	"time"
	"unicode"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

const (
	MaxRequestBytes          = 4096
	DefaultChallengeLifetime = 30 * time.Minute
	MaxIssueRequestsPerHour  = 3
	passwordResetPurpose     = store.ChallengePasswordReset
)

var (
	ErrConfiguration        = errors.New("invalid password recovery configuration")
	ErrUnavailable          = errors.New("password recovery is unavailable")
	ErrChallengeUnavailable = errors.New("password reset proof is unavailable")
	ErrStepUpRequired       = errors.New("password operation requires step-up proof")
	ErrPolicyDenied         = errors.New("password operation is not authorized")
)

type PasswordResetMaterialWriter interface {
	PutPasswordResetMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error
}

// Policy makes current step-up and account recovery policy an explicit runtime
// dependency. Implementations must inspect current policy in the provided tx.
type Policy interface {
	AuthorizePasswordChange(context.Context, *sql.Tx, identity.Principal) error
	AuthorizePasswordReset(context.Context, *sql.Tx, uuid.UUID) error
}

type Config struct {
	DB                                           *storage.DB
	Passwords                                    *password.Hasher
	Outbox                                       *sqlstore.Store
	Renderer                                     *deliveryemail.Renderer
	Materials                                    PasswordResetMaterialWriter
	Policy                                       Policy
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ApplicationOrigin                            string
	DevelopmentLoopback                          bool
	ChallengeLifetime                            time.Duration
}

// TxRunner is the transaction capability consumed by recovery.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type TxConfig struct {
	Passwords                                    *password.Hasher
	Outbox                                       *sqlstore.Store
	Renderer                                     *deliveryemail.Renderer
	Materials                                    PasswordResetMaterialWriter
	Policy                                       Policy
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ApplicationOrigin                            string
	DevelopmentLoopback                          bool
	ChallengeLifetime                            time.Duration
}

type Service struct {
	db     TxRunner
	cfg    TxConfig
	origin *url.URL
}

type Acknowledgement struct {
	Accepted bool `json:"accepted"`
}

type recoveryRequest struct {
	Email string `json:"email"`
}
type recoveryCompletion struct {
	ChallengeID string `json:"challenge_id"`
	Token       string `json:"token"`
	Password    string `json:"password"`
}
type passwordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
type response struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func New(cfg Config) (*Service, error) {
	return NewWithTxRunner(cfg.DB, TxConfig{
		Passwords: cfg.Passwords, Outbox: cfg.Outbox, Renderer: cfg.Renderer,
		Materials: cfg.Materials, Policy: cfg.Policy, InstallationID: cfg.InstallationID,
		ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID,
		ApplicationOrigin: cfg.ApplicationOrigin, DevelopmentLoopback: cfg.DevelopmentLoopback,
		ChallengeLifetime: cfg.ChallengeLifetime,
	})
}

// NewWithTxRunner constructs recovery without probing or owning the runner.
func NewWithTxRunner(db TxRunner, cfg TxConfig) (*Service, error) {
	if nilTxRunner(db) || cfg.Passwords == nil || cfg.Outbox == nil || cfg.Renderer == nil || cfg.Materials == nil || cfg.Policy == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrConfiguration
	}
	if cfg.ChallengeLifetime == 0 {
		cfg.ChallengeLifetime = DefaultChallengeLifetime
	}
	if cfg.ChallengeLifetime < deliveryemail.MinExpirySeconds*time.Second || cfg.ChallengeLifetime > deliveryemail.MaxExpirySeconds*time.Second || cfg.ChallengeLifetime%time.Second != 0 {
		return nil, ErrConfiguration
	}
	origin, err := deliveryemail.ParseApplicationOrigin(cfg.ApplicationOrigin, cfg.DevelopmentLoopback)
	if err != nil {
		return nil, ErrConfiguration
	}
	s := &Service{db: db, cfg: cfg, origin: origin}
	probeID, err := store.NewID()
	if err != nil {
		return nil, ErrUnavailable
	}
	probeRef := deliveryemail.SecretReference("material:" + probeID.String())
	probeURL := s.actionURL(probeID, strings.Repeat("A", 43))
	_, err = cfg.Renderer.Render(probeID, deliveryemail.Request{Template: deliveryemail.TemplatePasswordReset, MaterialRef: probeRef, ExpiresInSeconds: int64(cfg.ChallengeLifetime / time.Second)}, deliveryemail.PrivateMaterial{Recipient: "recovery-probe@example.invalid", ActionURL: probeURL})
	if err != nil {
		return nil, ErrConfiguration
	}
	return s, nil
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

// RequestHandler handles POST /forgot-password. It must be wrapped by the
// public recovery admission middleware before being exposed.
func (s *Service) RequestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		var in recoveryRequest
		if !decodeJSON(w, r, &in) {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		address, ok := normalizeAddress(in.Email)
		if !ok {
			writeError(w, r, http.StatusBadRequest, "identity.recovery_invalid", "Invalid recovery request.")
			return
		}
		// Check required capabilities before querying the address. That keeps
		// ordinary schema/role outages independent of account eligibility while
		// still failing visibly instead of claiming acceptance.
		if err := s.ready(r.Context()); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if err := s.request(r.Context(), address); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		writeAccepted(w, r)
	})
}

func (s *Service) ready(ctx context.Context) error {
	if s == nil || s.db == nil || ctx == nil {
		return ErrUnavailable
	}
	var ready bool
	err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var readOnly, defaultReadOnly, recovery bool
		if err := tx.QueryRowContext(ctx, `SELECT current_setting('transaction_read_only')::boolean,
			current_setting('default_transaction_read_only')::boolean, pg_is_in_recovery()`).Scan(&readOnly, &defaultReadOnly, &recovery); err != nil {
			return err
		}
		if readOnly || defaultReadOnly || recovery {
			return ErrUnavailable
		}
		return tx.QueryRowContext(ctx, `SELECT
			COALESCE(has_table_privilege(current_user,to_regclass('identity_persons'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_persons'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_emails'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_emails'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_credentials'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_credentials'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'INSERT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_challenges'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_sessions'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('identity_sessions'),'UPDATE'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('email_delivery_material'),'INSERT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('amos_jobs'),'SELECT'),false) AND
			COALESCE(has_table_privilege(current_user,to_regclass('amos_jobs'),'INSERT'),false)`).Scan(&ready)
	})
	if err != nil || !ready {
		return ErrUnavailable
	}
	return nil
}

// PreviewHandler handles GET/HEAD /reset-password. It validates proof without
// consuming it; the state-changing POST is separately admitted and consumes it.
func (s *Service) PreviewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.db == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		challengeID, challengeOK := parseID(r.URL.Query().Get("challenge"))
		token, digest, tokenErr := parseToken(r.URL.Query().Get("token"))
		_ = token
		available := false
		if challengeOK && tokenErr == nil {
			var err error
			available, err = s.preview(r.Context(), challengeID, digest[:])
			if err != nil {
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
				return
			}
		}
		writeResetPage(w, r.Method, available, challengeID, r.URL.Query().Get("token"))
	})
}

// CompleteHandler handles POST /reset-password and must be wrapped by
// PublicJSON(Recovery), which binds the password budget to challenge_id.
func (s *Service) CompleteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		var in recoveryCompletion
		if !decodeJSON(w, r, &in) {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		challengeID, ok := parseID(in.ChallengeID)
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "identity.recovery_unavailable", "Reset proof is unavailable.")
			return
		}
		_, digest, err := parseToken(in.Token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "identity.recovery_unavailable", "Reset proof is unavailable.")
			return
		}
		if err := s.complete(r.Context(), challengeID, digest[:], in.Password); err != nil {
			switch {
			case errors.Is(err, password.ErrInvalid):
				writeError(w, r, http.StatusBadRequest, "identity.password_invalid", "Password does not meet the current policy.")
			case errors.Is(err, ErrChallengeUnavailable), errors.Is(err, ErrPolicyDenied), errors.Is(err, ErrStepUpRequired):
				writeError(w, r, http.StatusUnauthorized, "identity.recovery_unavailable", "Reset proof is unavailable.")
			case errors.Is(err, ErrUnavailable):
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			default:
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			}
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
}

// PasswordChangeHandler handles POST /account/password and must be wrapped by
// CookieMutation(PasswordChange), which supplies the sealed current principal,
// validates CSRF/origin, and grants separate current/new password work budgets.
func (s *Service) PasswordChangeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.db == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed.")
			return
		}
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok || principal.InstallationID() != s.cfg.InstallationID || principal.ApplicationID() != s.cfg.ApplicationID || principal.EnvironmentID() != s.cfg.EnvironmentID || !validID(principal.PersonID()) {
			writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated", "Current authentication is unavailable.")
			return
		}
		var in passwordChange
		if !decodeJSON(w, r, &in) {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		if len(in.CurrentPassword) == 0 || len(in.CurrentPassword) > 512 || len(in.NewPassword) > 512 {
			writeError(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
			return
		}
		if err := s.change(r.Context(), principal, in.CurrentPassword, in.NewPassword); err != nil {
			switch {
			case errors.Is(err, password.ErrInvalid):
				writeError(w, r, http.StatusBadRequest, "identity.password_invalid", "Password does not meet the current policy.")
			case errors.Is(err, ErrChallengeUnavailable):
				writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated", "Current authentication is unavailable.")
			case errors.Is(err, ErrStepUpRequired), errors.Is(err, ErrPolicyDenied):
				writeError(w, r, http.StatusForbidden, "auth.forbidden", "Current password change policy requirements were not met.")
			case errors.Is(err, ErrUnavailable):
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			default:
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			}
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Service) request(ctx context.Context, address string) error {
	if s == nil || s.db == nil || ctx == nil {
		return ErrUnavailable
	}
	challengeID, err1 := store.NewID()
	requestID, err2 := store.NewID()
	if err1 != nil || err2 != nil {
		return ErrUnavailable
	}
	token, digest, err := newToken()
	if err != nil {
		return ErrUnavailable
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var personID, emailID uuid.UUID
		var displayAddress string
		err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,e.display_address
			FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id
			WHERE p.installation_id=$1 AND p.application_id=$2 AND p.state='active'
			  AND e.installation_id=$1 AND e.application_id=$2 AND e.comparison_key=$3
			  AND e.verified_at IS NOT NULL
			FOR UPDATE OF p,e`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&personID, &emailID, &displayAddress)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var recent int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges
			WHERE person_id=$1 AND email_id=$2 AND purpose=$3
			  AND created_at >= transaction_timestamp() - interval '1 hour'`, personID, emailID, passwordResetPurpose).Scan(&recent); err != nil {
			return err
		}
		if recent >= MaxIssueRequestsPerHour {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE identity_challenges SET consumed_at=transaction_timestamp()
			WHERE person_id=$1 AND email_id=$2 AND purpose=$3 AND consumed_at IS NULL`, personID, emailID, passwordResetPurpose); err != nil {
			return err
		}
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp() + make_interval(secs => $1)`, s.cfg.ChallengeLifetime.Seconds()).Scan(&expires); err != nil {
			return err
		}
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		if err := st.CreateChallenge(ctx, store.Challenge{ID: challengeID, PersonID: personID, EmailID: emailID, Purpose: passwordResetPurpose, Digest: digest[:], ExpiresAt: expires}); err != nil {
			return err
		}
		ref := deliveryemail.SecretReference("material:" + challengeID.String())
		material := deliveryemail.PrivateMaterial{Recipient: displayAddress, ActionURL: s.actionURL(challengeID, token)}
		if err := s.cfg.Materials.PutPasswordResetMaterial(ctx, tx, ref, material, expires); err != nil {
			return err
		}
		request := deliveryemail.Request{Template: deliveryemail.TemplatePasswordReset, MaterialRef: ref, ExpiresInSeconds: int64(s.cfg.ChallengeLifetime / time.Second)}
		_, err = deliveryemail.EnqueueTx(ctx, tx, s.cfg.Outbox, s.cfg.Renderer, s.cfg.InstallationID, s.cfg.ApplicationID, "password-reset:"+requestID.String(), request, expires)
		return err
	})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) preview(ctx context.Context, challengeID uuid.UUID, digest []byte) (bool, error) {
	var available bool
	err := s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM identity_challenges c
			JOIN identity_persons p ON p.id=c.person_id
			JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
			WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3
			  AND c.consumed_at IS NULL AND c.expires_at>transaction_timestamp()
			  AND p.installation_id=$4 AND p.application_id=$5 AND p.state='active'
			  AND e.installation_id=$4 AND e.application_id=$5 AND e.verified_at IS NOT NULL)`, challengeID, passwordResetPurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&available)
	})
	if err != nil {
		return false, ErrUnavailable
	}
	return available, nil
}

func (s *Service) complete(ctx context.Context, challengeID uuid.UUID, digest []byte, newPassword string) error {
	if s == nil || s.db == nil || ctx == nil {
		return ErrUnavailable
	}
	personID, emailID, ok, err := s.resetPreflight(ctx, challengeID, digest)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChallengeUnavailable
	}
	newHash, err := s.cfg.Passwords.Hash(ctx, "reset:"+challengeID.String(), newPassword)
	if err != nil {
		if errors.Is(err, password.ErrInvalid) {
			return err
		}
		return ErrUnavailable
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT p.state
			FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id
			WHERE p.id=$1 AND e.id=$2 AND p.installation_id=$3 AND p.application_id=$4
			  AND e.installation_id=$3 AND e.application_id=$4 AND e.verified_at IS NOT NULL
			FOR UPDATE OF p,e`, personID, emailID, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrChallengeUnavailable
			}
			return err
		}
		if state != "active" {
			return ErrChallengeUnavailable
		}
		consumed, err := st.ConsumeChallenge(ctx, challengeID, passwordResetPurpose, digest)
		if errors.Is(err, store.ErrChallengeUnavailable) {
			return ErrChallengeUnavailable
		}
		if err != nil {
			return err
		}
		if consumed.PersonID != personID || consumed.EmailID != emailID {
			return ErrChallengeUnavailable
		}
		var currentHash string
		err = tx.QueryRowContext(ctx, `SELECT p.state,c.verifier_hash
			FROM identity_persons p
			JOIN identity_emails e ON e.person_id=p.id AND e.verified_at IS NOT NULL
			JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
			WHERE p.id=$1 AND e.id=$2 AND p.installation_id=$3 AND p.application_id=$4
			  AND e.installation_id=$3 AND e.application_id=$4
			FOR UPDATE OF p,e,c`, personID, emailID, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&state, &currentHash)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrChallengeUnavailable
		}
		if err != nil {
			return err
		}
		if state != "active" || currentHash == "" {
			return ErrChallengeUnavailable
		}
		if err := s.cfg.Policy.AuthorizePasswordReset(ctx, tx, personID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE identity_credentials c SET verifier_hash=$1
			FROM identity_persons p
			WHERE c.person_id=$2 AND c.method='email_password' AND c.revoked_at IS NULL
			  AND p.id=c.person_id AND p.installation_id=$3 AND p.application_id=$4 AND p.state='active'`, newHash, personID, s.cfg.InstallationID, s.cfg.ApplicationID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return ErrChallengeUnavailable
		}
		if _, err := st.AdvanceSecurityEpoch(ctx, personID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at=transaction_timestamp()
			WHERE person_id=$1 AND revoked_at IS NULL`, personID)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrChallengeUnavailable) || errors.Is(err, ErrPolicyDenied) || errors.Is(err, ErrStepUpRequired) {
			return err
		}
		return ErrUnavailable
	}
	return nil
}

func (s *Service) resetPreflight(ctx context.Context, challengeID uuid.UUID, digest []byte) (uuid.UUID, uuid.UUID, bool, error) {
	var personID, emailID uuid.UUID
	err := s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT p.id,e.id
			FROM identity_challenges ch
			JOIN identity_persons p ON p.id=ch.person_id
			JOIN identity_emails e ON e.id=ch.email_id AND e.person_id=ch.person_id
			JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
			WHERE ch.id=$1 AND ch.purpose=$2 AND ch.token_digest=$3
			  AND ch.consumed_at IS NULL AND ch.expires_at>transaction_timestamp()
			  AND p.installation_id=$4 AND p.application_id=$5 AND p.state='active'
		  AND e.installation_id=$4 AND e.application_id=$5 AND e.verified_at IS NOT NULL`, challengeID, passwordResetPurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&personID, &emailID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, ErrUnavailable
	}
	return personID, emailID, true, nil
}

func (s *Service) change(ctx context.Context, principal identity.Principal, currentPassword, newPassword string) error {
	if s == nil || s.db == nil || ctx == nil {
		return ErrUnavailable
	}
	personID := principal.PersonID()
	var encoded string
	err := s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT c.verifier_hash
			FROM identity_persons p
			JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
			WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active'
			  AND EXISTS (SELECT 1 FROM identity_emails e WHERE e.person_id=p.id
			    AND e.installation_id=p.installation_id AND e.application_id=p.application_id AND e.verified_at IS NOT NULL)`, personID, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&encoded)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrChallengeUnavailable
	}
	if err != nil {
		return ErrUnavailable
	}
	verified, err := s.cfg.Passwords.Verify(ctx, "change-current:"+personID.String(), currentPassword, encoded, nil)
	if err != nil {
		if errors.Is(err, password.ErrInvalid) {
			return ErrChallengeUnavailable
		}
		return ErrUnavailable
	}
	if !verified.Verified {
		return ErrChallengeUnavailable
	}
	newHash, err := s.cfg.Passwords.Hash(ctx, "change-new:"+personID.String(), newPassword)
	if err != nil {
		if errors.Is(err, password.ErrInvalid) {
			return err
		}
		return ErrUnavailable
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var currentHash string
		var epoch int64
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT p.security_epoch,p.state,c.verifier_hash
			FROM identity_persons p
			JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
			WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3
			  AND EXISTS (SELECT 1 FROM identity_emails e WHERE e.person_id=p.id
			    AND e.installation_id=p.installation_id AND e.application_id=p.application_id AND e.verified_at IS NOT NULL)
			FOR UPDATE OF p,c`, personID, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&epoch, &state, &currentHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrChallengeUnavailable
			}
			return err
		}
		if state != "active" || epoch != principal.SecurityEpoch() || currentHash != encoded {
			return ErrChallengeUnavailable
		}
		if err := s.cfg.Policy.AuthorizePasswordChange(ctx, tx, principal); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE identity_credentials SET verifier_hash=$1
			WHERE person_id=$2 AND method='email_password' AND verifier_hash=$3 AND revoked_at IS NULL`, newHash, personID, encoded)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return ErrChallengeUnavailable
		}
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		if _, err := st.AdvanceSecurityEpoch(ctx, personID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at=transaction_timestamp()
			WHERE person_id=$1 AND revoked_at IS NULL`, personID)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrChallengeUnavailable) || errors.Is(err, ErrPolicyDenied) || errors.Is(err, ErrStepUpRequired) {
			return err
		}
		return ErrUnavailable
	}
	return nil
}

func (s *Service) actionURL(challengeID uuid.UUID, token string) string {
	action := *s.origin
	action.Path = "/reset-password"
	query := url.Values{}
	query.Set("challenge", challengeID.String())
	query.Set("token", token)
	action.RawQuery = query.Encode()
	return action.String()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r == nil || r.Body == nil || len(r.Header.Values("Content-Type")) != 1 || r.ContentLength > MaxRequestBytes {
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func normalizeAddress(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 {
		return "", false
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 64 || len(parts[1]) == 0 {
		return "", false
	}
	for _, ch := range value {
		if ch < 0x21 || ch > 0x7e || unicode.IsControl(ch) || strings.ContainsRune("<>() ,;:\\\"[]", ch) {
			return "", false
		}
	}
	return strings.ToLower(value), true
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

func writeAccepted(w http.ResponseWriter, r *http.Request) {
	id := requestID(w, r)
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}{"identity.password-recovery.accepted", "Request received. If the address belongs to an eligible account, reset instructions may be sent.", id})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id := requestID(w, r)
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Code: code, Message: message, RequestID: id})
}

func requestID(w http.ResponseWriter, r *http.Request) string {
	id := w.Header().Get("X-Request-ID")
	if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id && validID(parsed) {
		return id
	}
	generated, err := store.NewID()
	if err != nil {
		return "unavailable"
	}
	return generated.String()
}

var pageTemplate = template.Must(template.New("password-reset").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>Password reset</title><body><main><h1>Password reset</h1>{{if .Available}}<p>Choose a new password to continue.</p><form id="reset" method="post" action="/reset-password"><input type="hidden" name="challenge_id" value="{{.ChallengeID}}"><input type="hidden" name="token" value="{{.Token}}"><label>New password <input name="password" type="password" autocomplete="new-password" minlength="15" maxlength="128" required></label><button type="submit">Reset password</button></form><p id="result" aria-live="polite"></p><script nonce="{{.Nonce}}">document.getElementById('reset').addEventListener('submit',async function(event){event.preventDefault();const form=event.currentTarget;const values=new FormData(form);const body={challenge_id:values.get('challenge_id'),token:values.get('token'),'password':values.get('password')};try{const response=await fetch('/reset-password',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});document.getElementById('result').textContent=response.ok?'Password changed. You can sign in.':'This reset link is unavailable or expired.';form.hidden=true;}catch(_){document.getElementById('result').textContent='Password reset is temporarily unavailable.';}});</script>{{else}}<p>This reset link is invalid, expired, or already used.</p>{{end}}</main></body></html>`))

type pageData struct {
	Available   bool
	ChallengeID string
	Token       string
	Nonce       string
}

func writeResetPage(w http.ResponseWriter, method string, available bool, challengeID uuid.UUID, token string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	data := pageData{Available: available}
	if available {
		var nonce [18]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			writeError(w, nil, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
			return
		}
		data.ChallengeID = challengeID.String()
		data.Token = token
		data.Nonce = base64.RawURLEncoding.EncodeToString(nonce[:])
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+data.Nonce+"'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	}
	if method != http.MethodHead {
		if err := pageTemplate.Execute(w, data); err != nil {
			return
		}
	}
}

// Preview validates a reset proof without consuming it. Invalid or expired
// proofs share the same result; callers must not render the supplied token.
func (s *Service) Preview(ctx context.Context, challengeID uuid.UUID, token string) (bool, error) {
	if s == nil || s.db == nil || ctx == nil {
		return false, ErrUnavailable
	}
	id, ok := parseID(challengeID.String())
	_, digest, err := parseToken(token)
	if !ok || err != nil {
		return false, nil
	}
	return s.preview(ctx, id, digest[:])
}
