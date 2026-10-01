package mfa

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestT3_15_EnrollmentConfirmationAndReplayUseCurrentPassword(t *testing.T) {
	db, query := newMFADatabase(t)
	scope := createActivePerson(t, db)
	now := time.Unix(1_800_000_000, 0).UTC()
	primary := &testPrimary{now: now}
	sessions := &testSessionIssuer{}
	service, err := New(Config{DB: db, Vault: newTestVault(), Primary: primary, Policy: testPolicy{}, Sessions: sessions, Issuer: "AMOS Test", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := authproof.NewVerifiedCredential(scope.PersonID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
	if !ok {
		t.Fatal("failed to build test principal")
	}
	enrollment, err := service.BeginEnrollment(context.Background(), principal, "correct-current-password")
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), principal)
	if err != nil || !status.Pending || status.Enabled || status.PendingFactorID == nil || *status.PendingFactorID != enrollment.FactorID {
		t.Fatalf("pending status=%+v err=%v", status, err)
	}
	code, err := totp.GenerateCodeCustom(enrollment.Seed, now, totp.ValidateOpts{Period: uint(TOTPPeriod / time.Second), Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/account/mfa/totp/confirm", nil)
	input := proofRequest{FactorID: enrollment.FactorID, CurrentPassword: "correct-current-password", Code: code}
	if _, err := service.ConfirmEnrollment(context.Background(), principal, request, input); err != nil {
		t.Fatal(err)
	}
	if sessions.calls != 1 || sessions.proof.Assurance() != "aal2" || sessions.proof.Method() != "password+totp" || sessions.proof.PersonID() != scope.PersonID {
		t.Fatalf("step-up proof not minted from both factors: calls=%d proof=%+v", sessions.calls, sessions.proof)
	}
	status, err = service.Status(context.Background(), principal)
	if err != nil || !status.Enabled || status.Pending {
		t.Fatalf("active status=%+v err=%v", status, err)
	}
	var storedStep int64
	if err := query.QueryRow(`SELECT last_used_step FROM identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&storedStep); err != nil {
		t.Fatal(err)
	}
	if storedStep != now.Unix()/30 {
		t.Fatalf("confirmation did not consume its accepted step: %d", storedStep)
	}
	if _, err := service.Challenge(context.Background(), principal, request, input); !errors.Is(err, ErrReplay) {
		t.Fatalf("same step challenge err=%v, want replay denial", err)
	}
	if sessions.calls != 1 {
		t.Fatalf("replayed step minted session proof; calls=%d", sessions.calls)
	}
	// A fresh step with no current primary password still cannot consume or
	// promote the factor. The next correct primary proof can use the same code.
	now = now.Add(TOTPPeriod)
	newCode, err := totp.GenerateCodeCustom(enrollment.Seed, now, totp.ValidateOpts{Period: uint(TOTPPeriod / time.Second), Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	input.Code = newCode
	input.CurrentPassword = "wrong-current-password"
	if _, err := service.Challenge(context.Background(), principal, request, input); !errors.Is(err, ErrBadProof) {
		t.Fatalf("wrong current password err=%v", err)
	}
	input.CurrentPassword = "correct-current-password"
	if _, err := service.Challenge(context.Background(), principal, request, input); err != nil {
		t.Fatalf("fresh code after denied primary proof was consumed: %v", err)
	}
	if sessions.calls != 2 {
		t.Fatalf("valid combined proof did not rotate session: calls=%d", sessions.calls)
	}
}

func TestT3_15_VaultAndPolicyFailuresFailClosed(t *testing.T) {
	db, _ := newMFADatabase(t)
	scope := createActivePerson(t, db)
	now := time.Unix(1_800_000_000, 0).UTC()
	proof, err := authproof.NewVerifiedCredential(scope.PersonID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	principal, _ := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
	vault := newTestVault()
	policy := testPolicy{}
	sessions := &testSessionIssuer{}
	service, err := New(Config{DB: db, Vault: vault, Primary: &testPrimary{now: now}, Policy: policy, Sessions: sessions, Issuer: "AMOS Test", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := service.BeginEnrollment(context.Background(), principal, "correct-current-password")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCodeCustom(enrollment.Seed, now, totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	vault.failOpen = true
	if _, err := service.ConfirmEnrollment(context.Background(), principal, httptest.NewRequest(http.MethodPost, "/account/mfa/totp/confirm", nil), proofRequest{FactorID: enrollment.FactorID, CurrentPassword: "correct-current-password", Code: code}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("vault outage err=%v", err)
	}
	if sessions.calls != 0 {
		t.Fatal("vault outage issued a session")
	}
	factor := findFactor(t, db, scope, enrollment.FactorID)
	if factor.State != FactorPending {
		t.Fatalf("vault outage changed factor state: %+v", factor)
	}
}

func TestT3_15_HandlerBoundsBodyAndDisablesCaching(t *testing.T) {
	db, _ := newMFADatabase(t)
	scope := createActivePerson(t, db)
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(scope.PersonID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{DB: db, Vault: newTestVault(), Primary: &testPrimary{now: now}, Policy: testPolicy{}, Sessions: &testSessionIssuer{}, Issuer: "AMOS Test"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/account/mfa/totp/enroll", strings.NewReader(strings.Repeat("x", MaxRequestBytes+1)))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.ContextWithVerifiedCredential(request.Context(), proof))
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("oversized request response: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

type testPolicy struct{ unavailable, denied bool }

func (p testPolicy) Ready(context.Context, *sql.Tx) error {
	if p.unavailable {
		return ErrUnavailable
	}
	return nil
}
func (p testPolicy) AuthorizeMFA(context.Context, *sql.Tx, identity.Principal, string) error {
	if p.denied {
		return ErrDenied
	}
	return nil
}

type testPrimary struct{ now time.Time }

func (p *testPrimary) VerifyCurrentPassword(_ context.Context, _ *sql.Tx, principal identity.Principal, supplied string) (authproof.VerifiedCredential, error) {
	if supplied != "correct-current-password" {
		return authproof.VerifiedCredential{}, ErrBadProof
	}
	return authproof.NewVerifiedCredential(principal.PersonID(), principal.InstallationID(), principal.ApplicationID(), principal.EnvironmentID(), principal.SecurityEpoch(), "email_password", p.now, "aal1", p.now.Add(time.Hour))
}

type testSessionIssuer struct {
	calls int
	proof authproof.VerifiedCredential
}

func (s *testSessionIssuer) IssueForRequestTx(_ context.Context, _ *sql.Tx, proof authproof.VerifiedCredential, _ *http.Request) (session.Issued, error) {
	s.calls++
	s.proof = proof
	return session.Issued{Cookie: &http.Cookie{Name: "test", Value: "rotated", Expires: proof.AssuranceExpires()}, CSRFToken: "fixture-csrf", AssuranceExpires: proof.AssuranceExpires()}, nil
}

func (*testSessionIssuer) AllowsOrigin(*http.Request) bool { return true }

type vaultAAD struct {
	scope       Scope
	factor      uuid.UUID
	state       FactorState
	expiresUnix int64
	purpose     string
}
type vaultRecord struct {
	aad  vaultAAD
	seed []byte
}
type testVault struct {
	mu       sync.Mutex
	secrets  map[string]vaultRecord
	failOpen bool
}

func newTestVault() *testVault { return &testVault{secrets: make(map[string]vaultRecord)} }
func (v *testVault) Seal(_ context.Context, _ *sql.Tx, scope Scope, id uuid.UUID, state FactorState, expires time.Time, purpose string, plaintext []byte) ([]byte, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := string(nonce[:])
	v.mu.Lock()
	v.secrets[key] = vaultRecord{aad: makeVaultAAD(scope, id, state, expires, purpose), seed: append([]byte(nil), plaintext...)}
	v.mu.Unlock()
	return append([]byte(nil), nonce[:]...), nil
}
func (v *testVault) Open(_ context.Context, _ *sql.Tx, scope Scope, id uuid.UUID, state FactorState, expires time.Time, purpose string, ciphertext []byte) ([]byte, error) {
	if v.failOpen {
		return nil, errors.New("fixture vault unavailable")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	record, ok := v.secrets[string(ciphertext)]
	if !ok || record.aad != makeVaultAAD(scope, id, state, expires, purpose) {
		return nil, errors.New("fixture ciphertext unknown")
	}
	return append([]byte(nil), record.seed...), nil
}

func makeVaultAAD(scope Scope, id uuid.UUID, state FactorState, expires time.Time, purpose string) vaultAAD {
	var expiry int64
	if !expires.IsZero() {
		expiry = expires.UTC().UnixMicro()
	}
	return vaultAAD{scope: scope, factor: id, state: state, expiresUnix: expiry, purpose: purpose}
}
