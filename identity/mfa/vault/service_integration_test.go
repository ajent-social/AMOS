package vault_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/mfa"
	mfavault "github.com/ajent-social/amos/identity/mfa/vault"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestT3_15_ConcreteVaultUsesPersistedPendingExpiryAndActiveState(t *testing.T) {
	db, query, scope := newVaultTestDatabase(t)
	now := time.Date(2026, 10, 1, 19, 0, 0, 123456789, time.UTC)
	primary := &primaryVerifier{clock: &now}
	issuer := &sessionIssuer{}
	seedVault, err := mfavault.NewKeyring("local-v1", map[string][]byte{"local-v1": bytes.Repeat([]byte{0x63}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	service, err := mfa.New(mfa.Config{DB: db, Vault: seedVault, Primary: primary, Policy: policy{}, Sessions: issuer, Issuer: "AMOS Test", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	proofTime := now.UTC().Truncate(time.Microsecond)
	credential, err := authproof.NewVerifiedCredential(scope.PersonID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, 0, "email_password", proofTime, "aal1", proofTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), credential))
	if !ok {
		t.Fatal("failed to build test principal")
	}
	enrollment, err := service.BeginEnrollment(context.Background(), principal, "correct-current-password")
	if err != nil {
		t.Fatal(err)
	}
	var storedExpiry time.Time
	if err := query.QueryRow(`SELECT expires_at FROM identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&storedExpiry); err != nil {
		t.Fatal(err)
	}
	expectedExpiry := now.UTC().Truncate(time.Microsecond).Add(30 * time.Minute)
	if !storedExpiry.Equal(expectedExpiry) || !enrollment.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("pending expiry mismatch: db=%s response=%s expected=%s", storedExpiry, enrollment.ExpiresAt, expectedExpiry)
	}

	handler := service.Handler()
	confirmCode, err := totp.GenerateCodeCustom(enrollment.Seed, now, totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	confirmBody, err := json.Marshal(map[string]any{"factor_id": enrollment.FactorID, "current_password": "correct-current-password", "code": confirmCode})
	if err != nil {
		t.Fatal(err)
	}
	confirm := requestWithPrincipal(http.MethodPost, "/account/mfa/totp/confirm", confirmBody, credential)
	confirmed := httptest.NewRecorder()
	handler.ServeHTTP(confirmed, confirm)
	if confirmed.Code != http.StatusOK || issuer.calls != 1 {
		t.Fatalf("pending ciphertext confirmation: status=%d calls=%d body=%s", confirmed.Code, issuer.calls, confirmed.Body.String())
	}

	now = now.Add(30 * time.Second)
	challengeCode, err := totp.GenerateCodeCustom(enrollment.Seed, now, totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	challengeBody, err := json.Marshal(map[string]string{"current_password": "correct-current-password", "code": challengeCode})
	if err != nil {
		t.Fatal(err)
	}
	challenge := requestWithPrincipal(http.MethodPost, "/account/mfa/totp/challenge", challengeBody, credential)
	challenged := httptest.NewRecorder()
	handler.ServeHTTP(challenged, challenge)
	if challenged.Code != http.StatusOK || issuer.calls != 2 {
		t.Fatalf("active ciphertext challenge: status=%d calls=%d body=%s", challenged.Code, issuer.calls, challenged.Body.String())
	}
}

type testScope struct{ InstallationID, ApplicationID, EnvironmentID, PersonID uuid.UUID }

func newVaultTestDatabase(t *testing.T) (*storage.DB, *sql.DB, testScope) {
	t.Helper()
	query, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	values.Set("search_path", schema)
	parsed.RawQuery = values.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	protectionSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "fragments", "identity-protection.sql"))
	if err != nil {
		t.Fatal(err)
	}
	assurance, err := migrations.SessionAssurance(3)
	if err != nil {
		t.Fatal(err)
	}
	mfaProtection, err := migrations.MFAProtection(4)
	if err != nil {
		t.Fatal(err)
	}
	factorSchema, err := mfa.Fragment(5)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}},
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 2, Name: "identity_protection", SQL: string(protectionSQL)}}},
		assurance,
		mfaProtection,
		factorSchema,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	scope := testScope{InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t), PersonID: newID(t)}
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, scope.PersonID, scope.InstallationID, scope.ApplicationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return db, query, scope
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func requestWithPrincipal(method, path string, body []byte, credential authproof.VerifiedCredential) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://mfa.example")
	return req.WithContext(identity.ContextWithVerifiedCredential(req.Context(), credential))
}

type policy struct{}

func (policy) Ready(context.Context, *sql.Tx) error                                    { return nil }
func (policy) AuthorizeMFA(context.Context, *sql.Tx, identity.Principal, string) error { return nil }

type primaryVerifier struct{ clock *time.Time }

func (v *primaryVerifier) VerifyCurrentPassword(_ context.Context, _ *sql.Tx, principal identity.Principal, supplied string) (authproof.VerifiedCredential, error) {
	if supplied != "correct-current-password" {
		return authproof.VerifiedCredential{}, mfa.ErrBadProof
	}
	now := v.clock.UTC().Truncate(time.Microsecond)
	return authproof.NewVerifiedCredential(principal.PersonID(), principal.InstallationID(), principal.ApplicationID(), principal.EnvironmentID(), principal.SecurityEpoch(), "email_password", now, "aal1", now.Add(time.Hour))
}

type sessionIssuer struct{ calls int }

func (*sessionIssuer) AllowsOrigin(*http.Request) bool { return true }
func (s *sessionIssuer) IssueForRequestTx(_ context.Context, _ *sql.Tx, proof authproof.VerifiedCredential, _ *http.Request) (session.Issued, error) {
	s.calls++
	return session.Issued{Cookie: &http.Cookie{Name: "fixture", Value: "rotated", Expires: proof.AssuranceExpires()}, CSRFToken: "fixture-csrf", AssuranceExpires: proof.AssuranceExpires()}, nil
}
