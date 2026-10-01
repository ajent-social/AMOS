package mfa

import (
	"bytes"
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	MaxRequestBytes        = 4096
	DefaultFreshness       = 15 * time.Minute
	DefaultPendingLifetime = 30 * time.Minute
	DefaultCodeLockout     = 15 * time.Minute
	DefaultMaxAttempts     = 5
	TOTPPeriod             = 30 * time.Second
	TOTPSkew               = 1
	AAL2Lifetime           = 15 * time.Minute
	seedPurpose            = "totp.seed.v1"
)

var (
	ErrConfiguration = errors.New("invalid MFA configuration")
	ErrUnavailable   = errors.New("MFA unavailable")
	ErrDenied        = errors.New("MFA operation denied")
	ErrBadProof      = errors.New("primary proof rejected")
	ErrBadCode       = errors.New("MFA code rejected")
	ErrReplay        = errors.New("MFA code already used")
)

type SeedVault interface {
	Seal(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error)
	Open(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error)
}

type PrimaryProofVerifier interface {
	VerifyCurrentPassword(context.Context, *sql.Tx, identity.Principal, string) (authproof.VerifiedCredential, error)
}

type Policy interface {
	Ready(context.Context, *sql.Tx) error
	AuthorizeMFA(context.Context, *sql.Tx, identity.Principal, string) error
}

type SessionIssuer interface {
	IssueForRequestTx(context.Context, *sql.Tx, authproof.VerifiedCredential, *http.Request) (session.Issued, error)
}

type Config struct {
	DB       *storage.DB
	Vault    SeedVault
	Primary  PrimaryProofVerifier
	Policy   Policy
	Sessions SessionIssuer
	Issuer   string
	Now      func() time.Time
}

type Service struct{ cfg Config }

func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Vault == nil || cfg.Primary == nil || cfg.Policy == nil || cfg.Sessions == nil || cfg.Issuer == "" || len(cfg.Issuer) > 64 || strings.TrimSpace(cfg.Issuer) != cfg.Issuer {
		return nil, ErrConfiguration
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg}, nil
}

type Enrollment struct {
	FactorID   uuid.UUID `json:"factor_id"`
	Seed       string    `json:"secret"`
	OTPAuthURI string    `json:"otpauth_uri"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type FactorStatus struct {
	Enabled         bool       `json:"enabled"`
	Pending         bool       `json:"pending"`
	PendingFactorID *uuid.UUID `json:"pending_factor_id,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type beginRequest struct {
	CurrentPassword string `json:"current_password"`
}
type proofRequest struct {
	FactorID        uuid.UUID `json:"factor_id,omitempty"`
	CurrentPassword string    `json:"current_password"`
	Code            string    `json:"code"`
}

func (s *Service) BeginEnrollment(ctx context.Context, principal identity.Principal, currentPassword string) (Enrollment, error) {
	if s == nil || s.cfg.DB == nil || ctx == nil || currentPassword == "" || len(currentPassword) > 512 {
		return Enrollment{}, ErrUnavailable
	}
	scope := scopeFor(principal)
	if !validScope(scope) {
		return Enrollment{}, ErrDenied
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: s.cfg.Issuer, AccountName: scope.PersonID.String(), Period: uint(TOTPPeriod / time.Second), SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return Enrollment{}, ErrUnavailable
	}
	factorID, err := uuid.NewV7()
	if err != nil || !validID(factorID) {
		return Enrollment{}, ErrUnavailable
	}
	now := s.cfg.Now().UTC().Truncate(time.Microsecond)
	seed := []byte(key.Secret())
	defer wipe(seed)
	var sealed []byte
	var primary authproof.VerifiedCredential
	err = s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.cfg.Policy.Ready(ctx, tx); err != nil {
			return ErrUnavailable
		}
		if err := s.cfg.Policy.AuthorizeMFA(ctx, tx, principal, "totp.enroll"); err != nil {
			return ErrDenied
		}
		var err error
		primary, err = s.cfg.Primary.VerifyCurrentPassword(ctx, tx, principal, currentPassword)
		if err != nil {
			return mapPrimaryError(err)
		}
		// Primary verification can perform an expensive password check. Sample
		// freshness after it returns, then normalize persisted timestamps.
		now = s.cfg.Now().UTC()
		if err := validatePrimary(primary, principal, now); err != nil {
			return err
		}
		now = now.Truncate(time.Microsecond)
		sealed, err = s.cfg.Vault.Seal(ctx, tx, scope, factorID, FactorPending, now.Add(DefaultPendingLifetime), seedPurpose, seed)
		if err != nil || len(sealed) < 32 || len(sealed) > 4096 {
			return ErrUnavailable
		}
		st, err := NewStore(tx)
		if err != nil {
			return ErrUnavailable
		}
		return st.CreatePending(ctx, Factor{ID: factorID, Scope: scope, SeedCiphertext: sealed, State: FactorPending}, principal.SecurityEpoch(), now)
	})
	if err != nil {
		return Enrollment{}, classify(err)
	}
	return Enrollment{FactorID: factorID, Seed: key.Secret(), OTPAuthURI: key.URL(), ExpiresAt: now.Add(DefaultPendingLifetime)}, nil
}

func (s *Service) Status(ctx context.Context, principal identity.Principal) (FactorStatus, error) {
	if s == nil || s.cfg.DB == nil || ctx == nil {
		return FactorStatus{}, ErrUnavailable
	}
	scope := scopeFor(principal)
	if !validScope(scope) {
		return FactorStatus{}, ErrDenied
	}
	var result FactorStatus
	err := s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.cfg.Policy.Ready(ctx, tx); err != nil {
			return ErrUnavailable
		}
		if err := s.cfg.Policy.AuthorizeMFA(ctx, tx, principal, "totp.read"); err != nil {
			return ErrDenied
		}
		st, err := NewStore(tx)
		if err != nil {
			return ErrUnavailable
		}
		_, err = st.FindCurrent(ctx, scope, FactorActive)
		if err == nil {
			result.Enabled = true
		} else if !errors.Is(err, ErrFactorAbsent) {
			return err
		}
		pending, err := st.FindCurrent(ctx, scope, FactorPending)
		if err == nil && pending.ExpiresAt != nil && pending.ExpiresAt.After(s.cfg.Now().UTC()) {
			result.Pending = true
			result.PendingFactorID = &pending.ID
			result.ExpiresAt = pending.ExpiresAt
		} else if err != nil && !errors.Is(err, ErrFactorAbsent) {
			return err
		}
		return nil
	})
	if err != nil {
		return FactorStatus{}, classify(err)
	}
	return result, nil
}

func (s *Service) ConfirmEnrollment(ctx context.Context, principal identity.Principal, r *http.Request, input proofRequest) (session.Issued, error) {
	return s.verifyAndStepUp(ctx, principal, r, input, true)
}

func (s *Service) Challenge(ctx context.Context, principal identity.Principal, r *http.Request, input proofRequest) (session.Issued, error) {
	return s.verifyAndStepUp(ctx, principal, r, input, false)
}

func (s *Service) verifyAndStepUp(ctx context.Context, principal identity.Principal, r *http.Request, input proofRequest, pending bool) (session.Issued, error) {
	if s == nil || s.cfg.DB == nil || ctx == nil || r == nil || input.CurrentPassword == "" || len(input.CurrentPassword) > 512 || !validTOTPCode(input.Code) || (pending && !validID(input.FactorID)) {
		return session.Issued{}, ErrBadCode
	}
	scope := scopeFor(principal)
	if !validScope(scope) {
		return session.Issued{}, ErrDenied
	}
	now := s.cfg.Now().UTC().Truncate(time.Microsecond)
	var issued session.Issued
	var result error
	err := s.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.cfg.Policy.Ready(ctx, tx); err != nil {
			return ErrUnavailable
		}
		action := "totp.challenge"
		if pending {
			action = "totp.confirm"
		}
		if err := s.cfg.Policy.AuthorizeMFA(ctx, tx, principal, action); err != nil {
			return ErrDenied
		}
		primary, err := s.cfg.Primary.VerifyCurrentPassword(ctx, tx, principal, input.CurrentPassword)
		if err != nil {
			return mapPrimaryError(err)
		}
		// Primary verification can perform an expensive password check. Sample
		// freshness after it returns, then normalize persisted timestamps.
		now = s.cfg.Now().UTC()
		if err := validatePrimary(primary, principal, now); err != nil {
			return err
		}
		st, err := NewStore(tx)
		if err != nil {
			return ErrUnavailable
		}
		now = now.Truncate(time.Microsecond)
		var factor Factor
		if pending {
			factor, err = st.Find(ctx, scope, input.FactorID)
			if errors.Is(err, ErrFactorAbsent) {
				return ErrDenied
			}
			if err != nil {
				return ErrUnavailable
			}
			if factor.State != FactorPending || factor.ExpiresAt == nil || !factor.ExpiresAt.After(now) || factor.PendingSecurityEpoch == nil || *factor.PendingSecurityEpoch != principal.SecurityEpoch() {
				return ErrDenied
			}
		} else {
			factor, err = st.FindCurrent(ctx, scope, FactorActive)
			if errors.Is(err, ErrFactorAbsent) {
				return ErrDenied
			}
			if err != nil {
				return ErrUnavailable
			}
		}
		if st.Locked(factor, now) {
			return ErrBadCode
		}
		var aadExpiry time.Time
		if factor.State == FactorPending && factor.ExpiresAt != nil {
			aadExpiry = *factor.ExpiresAt
		}
		seed, err := s.cfg.Vault.Open(ctx, tx, scope, factor.ID, factor.State, aadExpiry, seedPurpose, factor.SeedCiphertext)
		if err != nil || len(seed) == 0 || len(seed) > 256 {
			return ErrUnavailable
		}
		defer wipe(seed)
		step, ok := matchingStep(input.Code, string(seed), now)
		if !ok {
			if err := st.RecordFailure(ctx, scope, factor.ID, now); err != nil {
				return ErrUnavailable
			}
			result = ErrBadCode
			return nil // Commit the failure count before returning denial.
		}
		if pending {
			activeCiphertext, sealErr := s.cfg.Vault.Seal(ctx, tx, scope, factor.ID, FactorActive, time.Time{}, seedPurpose, seed)
			if sealErr != nil || len(activeCiphertext) < 32 || len(activeCiphertext) > 4096 {
				return ErrUnavailable
			}
			err = st.ActivatePendingAndConsumeStep(ctx, scope, factor.ID, principal.SecurityEpoch(), step, activeCiphertext, now)
		} else {
			var accepted bool
			accepted, err = st.AcceptStep(ctx, scope, factor.ID, step, now)
			if err == nil && !accepted {
				if e := st.RecordFailure(ctx, scope, factor.ID, now); e != nil {
					return ErrUnavailable
				}
				result = ErrReplay
				return nil
			}
		}
		if err != nil {
			return err
		}
		stepUp, err := authproof.NewVerifiedCredential(principal.PersonID(), principal.InstallationID(), principal.ApplicationID(), principal.EnvironmentID(), principal.SecurityEpoch(), "password+totp", now, "aal2", now.Add(AAL2Lifetime))
		if err != nil {
			return ErrUnavailable
		}
		issued, err = s.cfg.Sessions.IssueForRequestTx(ctx, tx, stepUp, r)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return session.Issued{}, classify(err)
	}
	if result != nil {
		return session.Issued{}, result
	}
	return issued, nil
}

func validatePrimary(proof authproof.VerifiedCredential, principal identity.Principal, now time.Time) error {
	if proof.PersonID() != principal.PersonID() || proof.InstallationID() != principal.InstallationID() || proof.ApplicationID() != principal.ApplicationID() || proof.EnvironmentID() != principal.EnvironmentID() || proof.SecurityEpoch() != principal.SecurityEpoch() || proof.Method() != "email_password" || proof.Assurance() != "aal1" || proof.AuthenticatedAt().After(now) || now.Sub(proof.AuthenticatedAt()) > DefaultFreshness || proof.AssuranceExpires().Before(now) {
		return ErrBadProof
	}
	return nil
}

func matchingStep(code, encodedSeed string, now time.Time) (int64, bool) {
	if !validTOTPCode(code) || encodedSeed == "" || len(encodedSeed) > 256 {
		return 0, false
	}
	base := now.Unix() / int64(TOTPPeriod/time.Second)
	options := totp.ValidateOpts{Period: uint(TOTPPeriod / time.Second), Skew: TOTPSkew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	var matched int64 = -1
	for step := base - TOTPSkew; step <= base+TOTPSkew; step++ {
		if step < 0 {
			continue
		}
		candidate, err := totp.GenerateCodeCustom(encodedSeed, time.Unix(step*int64(TOTPPeriod/time.Second), 0), options)
		if err == nil && hmac.Equal([]byte(candidate), []byte(code)) {
			matched = step // Select the newest matching step if a 6-digit collision occurs.
		}
	}
	return matched, matched >= 0
}

func validTOTPCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func scopeFor(principal identity.Principal) Scope {
	return Scope{InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID(), EnvironmentID: principal.EnvironmentID(), PersonID: principal.PersonID()}
}

func mapPrimaryError(err error) error {
	if errors.Is(err, ErrBadProof) || errors.Is(err, ErrDenied) || errors.Is(err, primaryproof.ErrUnauthenticated) {
		return ErrBadProof
	}
	return ErrUnavailable
}

func classify(err error) error {
	switch {
	case errors.Is(err, ErrDenied), errors.Is(err, ErrBadProof), errors.Is(err, ErrBadCode), errors.Is(err, ErrReplay):
		return err
	case errors.Is(err, ErrFactorExists):
		return ErrFactorExists
	default:
		return ErrUnavailable
	}
}

func (s *Service) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Service) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "auth.unauthenticated")
		return
	}
	methodAllowed := r.Method == http.MethodPost || r.Method == http.MethodGet && r.URL.Path == "/account/mfa/totp"
	if !methodAllowed {
		w.Header().Set("Allow", "GET, POST")
		writeJSONError(w, http.StatusMethodNotAllowed, "method.not_allowed")
		return
	}
	if s == nil || s.cfg.Sessions == nil || (r.Method == http.MethodPost && !s.SessionsAllowsOrigin(r)) {
		writeJSONError(w, http.StatusForbidden, "request.origin_denied")
		return
	}
	if r.Method == http.MethodGet {
		status, err := s.Status(r.Context(), principal)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	if r.URL.RawQuery != "" {
		writeJSONError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		writeJSONError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	if r.Body == nil {
		writeJSONError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request.too_large")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	switch r.URL.Path {
	case "/account/mfa/totp/enroll":
		var input beginRequest
		if decodePayload(body, &input) != nil {
			writeJSONError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		result, err := s.BeginEnrollment(r.Context(), principal, input.CurrentPassword)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	case "/account/mfa/totp/confirm", "/account/mfa/totp/challenge":
		var input proofRequest
		if decodePayload(body, &input) != nil {
			writeJSONError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		var issued session.Issued
		var err error
		if r.URL.Path == "/account/mfa/totp/confirm" {
			issued, err = s.ConfirmEnrollment(r.Context(), principal, r, input)
		} else {
			issued, err = s.Challenge(r.Context(), principal, r, input)
		}
		if err != nil {
			writeServiceError(w, err)
			return
		}
		if issued.Cookie == nil || issued.CSRFToken == "" || issued.AssuranceExpires.IsZero() {
			writeJSONError(w, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		http.SetCookie(w, issued.Cookie)
		w.Header().Set("X-CSRF-Token", issued.CSRFToken)
		writeJSON(w, http.StatusOK, map[string]any{"assurance": "aal2", "expires_at": issued.AssuranceExpires})
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) SessionsAllowsOrigin(r *http.Request) bool {
	checker, ok := s.cfg.Sessions.(interface{ AllowsOrigin(*http.Request) bool })
	return ok && checker.AllowsOrigin(r)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	requestID := uuid.NewString()
	w.Header().Set("X-Request-ID", requestID)
	writeJSON(w, status, map[string]string{"code": code, "message": "request could not be completed", "request_id": requestID})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDenied):
		writeJSONError(w, http.StatusForbidden, "auth.proof_denied")
	case errors.Is(err, ErrBadProof), errors.Is(err, ErrBadCode), errors.Is(err, ErrReplay):
		writeJSONError(w, http.StatusUnauthorized, "auth.factor_invalid")
	case errors.Is(err, ErrFactorExists):
		writeJSONError(w, http.StatusConflict, "identity.factor_exists")
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "dependency.unavailable")
	}
}

func decodePayload(payload []byte, target any) error {
	if !json.Valid(payload) {
		return errors.New("invalid JSON request")
	}
	keys := json.NewDecoder(bytes.NewReader(payload))
	opening, err := keys.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("MFA request must be an object")
	}
	seen := make(map[string]struct{})
	for keys.More() {
		token, err := keys.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return errors.New("invalid MFA request key")
		}
		if _, exists := seen[key]; exists {
			return errors.New("duplicate MFA request key")
		}
		seen[key] = struct{}{}
		var raw json.RawMessage
		if err := keys.Decode(&raw); err != nil {
			return err
		}
	}
	if _, err := keys.Token(); err != nil {
		return err
	}
	if err := keys.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
