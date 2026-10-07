package mfa

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	_ func(Config) (*Service, error)             = New
	_ func(TxRunner, TxConfig) (*Service, error) = NewWithTxRunner
	_ TxRunner                                   = (*storage.DB)(nil)
	_ TxRunner                                   = (*storage.RuntimeDB)(nil)
)

type mfaRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f mfaRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type mfaPointerRunner struct{}

func (*mfaPointerRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type mfaMapRunner map[string]int

func (mfaMapRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type mfaSliceRunner []int

func (mfaSliceRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type mfaChannelRunner chan int

func (mfaChannelRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type mfaValueRunner struct{}

func (mfaValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

func mfaTxConfig() TxConfig {
	return TxConfig{Vault: newTestVault(), Primary: &testPrimary{}, Policy: testPolicy{}, Sessions: &testSessionIssuer{}, Issuer: "AMOS Test"}
}

func TestMFATxRunnerNilAdmission(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   TxRunner
	}{
		{"interface", nil}, {"pointer", (*mfaPointerRunner)(nil)}, {"map", mfaMapRunner(nil)},
		{"slice", mfaSliceRunner(nil)}, {"channel", mfaChannelRunner(nil)}, {"function", mfaRunnerFunc(nil)},
		{"legacy", (*storage.DB)(nil)}, {"runtime", (*storage.RuntimeDB)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mfaTxConfig()
			cfg.Now = func() time.Time { t.Fatal("constructor called clock"); return time.Time{} }
			got, err := NewWithTxRunner(tc.db, cfg)
			if got != nil || !errors.Is(err, ErrConfiguration) {
				t.Fatal("nil runner admitted")
			}
		})
	}
	cfg := mfaTxConfig()
	if got, err := New(Config{nil, cfg.Vault, cfg.Primary, cfg.Policy, cfg.Sessions, cfg.Issuer, cfg.Now}); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("legacy nil admitted")
	}
}

func TestMFATxRunnerCompatibilityAndDefaults(t *testing.T) {
	cfg := mfaTxConfig()
	legacy := Config{&storage.DB{}, cfg.Vault, cfg.Primary, cfg.Policy, cfg.Sessions, cfg.Issuer, cfg.Now}
	names := []string{"DB", "Vault", "Primary", "Policy", "Sessions", "Issuer", "Now"}
	typ := reflect.TypeOf(legacy)
	txType := reflect.TypeOf(cfg)
	if typ.NumField() != len(names) || txType.NumField() != len(names)-1 {
		t.Fatal("configuration shape changed")
	}
	for i, name := range names {
		if typ.Field(i).Name != name {
			t.Fatal("legacy field order changed")
		}
		if i > 0 && (txType.Field(i-1).Name != name || txType.Field(i-1).Type != typ.Field(i).Type) {
			t.Fatal("TxConfig differs beyond DB removal")
		}
	}
	for _, runner := range []TxRunner{mfaValueRunner{}, &mfaPointerRunner{}, mfaMapRunner{}, mfaSliceRunner{}, make(mfaChannelRunner), mfaRunnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { panic("constructor called runner") }), &storage.DB{}, &storage.RuntimeDB{}} {
		svc, err := NewWithTxRunner(runner, cfg)
		if err != nil || svc == nil {
			t.Fatalf("non-nil runner rejected: %v", err)
		}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*TxConfig)
		valid bool
	}{
		{"valid", func(*TxConfig) {}, true},
		{"vault", func(c *TxConfig) { c.Vault = nil }, false},
		{"primary", func(c *TxConfig) { c.Primary = nil }, false},
		{"policy", func(c *TxConfig) { c.Policy = nil }, false},
		{"sessions", func(c *TxConfig) { c.Sessions = nil }, false},
		{"issuer empty", func(c *TxConfig) { c.Issuer = "" }, false},
		{"issuer space", func(c *TxConfig) { c.Issuer = " AMOS" }, false},
		{"issuer long", func(c *TxConfig) { c.Issuer = strings.Repeat("a", 65) }, false},
		{"issuer boundary", func(c *TxConfig) { c.Issuer = strings.Repeat("a", 64) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := cfg
			tc.edit(&candidate)
			old, oldErr := New(Config{legacy.DB, candidate.Vault, candidate.Primary, candidate.Policy, candidate.Sessions, candidate.Issuer, candidate.Now})
			fresh, newErr := NewWithTxRunner(mfaValueRunner{}, candidate)
			if (oldErr == nil) != tc.valid || (newErr == nil) != tc.valid || (old != nil) != tc.valid || (fresh != nil) != tc.valid {
				t.Fatal("constructor validation diverged")
			}
			if !tc.valid && (!errors.Is(oldErr, ErrConfiguration) || !errors.Is(newErr, ErrConfiguration)) {
				t.Fatal("configuration sentinel changed")
			}
		})
	}
	before := time.Now()
	old, err := New(legacy)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewWithTxRunner(mfaValueRunner{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range []*Service{old, fresh} {
		if svc.cfg.Now == nil {
			t.Fatal("default clock absent")
		}
		if now := svc.cfg.Now(); now.Before(before) || now.After(time.Now()) {
			t.Fatal("default clock is not current time")
		}
		if svc.cfg.Vault != cfg.Vault || svc.cfg.Primary != cfg.Primary || svc.cfg.Policy != cfg.Policy || svc.cfg.Sessions != cfg.Sessions || svc.cfg.Issuer != cfg.Issuer {
			t.Fatal("configuration mapping changed")
		}
	}
	calls := 0
	cfg.Now = func() time.Time { calls++; return time.Unix(123, 0) }
	fresh, err = NewWithTxRunner(mfaValueRunner{}, cfg)
	if err != nil || calls != 0 {
		t.Fatal("constructor called configured clock")
	}
	if !fresh.cfg.Now().Equal(time.Unix(123, 0)) || calls != 1 {
		t.Fatal("configured clock replaced")
	}
	// Existing dependency interface admission stays unchanged: this amendment only
	// strengthens the runner's nil policy, not unrelated interface semantics.
	cfg = mfaTxConfig()
	cfg.Vault = (*testVault)(nil)
	cfg.Primary = (*testPrimary)(nil)
	cfg.Sessions = (*testSessionIssuer)(nil)
	if _, err := NewWithTxRunner(mfaValueRunner{}, cfg); err != nil {
		t.Fatal("unrelated typed-nil policy changed")
	}
}

func TestMFATxRunnerOptionsAndFailClosed(t *testing.T) {
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(newID(t), newID(t), newID(t), newID(t), 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
	if !ok {
		t.Fatal("principal absent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	runner := mfaRunnerFunc(func(got context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
		calls++
		if got != ctx || opts != nil || fn == nil {
			t.Fatal("transaction context/options/callback changed")
		}
		return errors.New("runner unavailable")
	})
	svc, err := NewWithTxRunner(runner, mfaTxConfig())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("constructor performed I/O")
	}
	if got, err := svc.BeginEnrollment(ctx, principal, "current-password"); !errors.Is(err, ErrUnavailable) || got.Seed != "" || got.OTPAuthURI != "" {
		t.Fatal("failed enrollment exposed material")
	}
	if got, err := svc.Status(ctx, principal); !errors.Is(err, ErrUnavailable) || got.Enabled || got.Pending {
		t.Fatal("status did not fail closed")
	}
	req := httptest.NewRequest(http.MethodPost, "/account/mfa/totp/confirm", nil)
	input := proofRequest{FactorID: newID(t), CurrentPassword: "current-password", Code: "123456"}
	for _, op := range []func(context.Context, identity.Principal, *http.Request, proofRequest) (session.Issued, error){svc.ConfirmEnrollment, svc.Challenge} {
		got, err := op(ctx, principal, req, input)
		if !errors.Is(err, ErrUnavailable) || got.Cookie != nil || got.CSRFToken != "" || !got.AssuranceExpires.IsZero() {
			t.Fatal("failed transaction exposed session")
		}
	}
	if calls != 4 {
		t.Fatalf("runner calls=%d want 4", calls)
	}
	if !svc.SessionsAllowsOrigin(req) {
		t.Fatal("optional origin checker lost")
	}
	cfg := mfaTxConfig()
	cfg.Sessions = mfaIssuerWithoutOrigin{}
	svc, err = NewWithTxRunner(runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if svc.SessionsAllowsOrigin(req) {
		t.Fatal("missing optional origin checker allowed")
	}
}

type mfaIssuerWithoutOrigin struct{}

func (mfaIssuerWithoutOrigin) IssueForRequestTx(context.Context, *sql.Tx, authproof.VerifiedCredential, *http.Request) (session.Issued, error) {
	panic("constructor called sessions")
}

// A single poison dependency implements each existing dependency interface, so
// even an attempted constructor readiness probe fails immediately.
type mfaPoisonDependency struct{}

func (mfaPoisonDependency) Seal(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error) {
	panic("constructor called vault")
}
func (mfaPoisonDependency) Open(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error) {
	panic("constructor called vault")
}
func (mfaPoisonDependency) VerifyCurrentPassword(context.Context, *sql.Tx, identity.Principal, string) (authproof.VerifiedCredential, error) {
	panic("constructor called primary verifier")
}
func (mfaPoisonDependency) Ready(context.Context, *sql.Tx) error {
	panic("constructor called policy")
}
func (mfaPoisonDependency) AuthorizeMFA(context.Context, *sql.Tx, identity.Principal, string) error {
	panic("constructor called policy")
}
func (mfaPoisonDependency) IssueForRequestTx(context.Context, *sql.Tx, authproof.VerifiedCredential, *http.Request) (session.Issued, error) {
	panic("constructor called session issuer")
}

func TestMFATxRunnerConstructorDoesNotCallDependencies(t *testing.T) {
	p := mfaPoisonDependency{}
	cfg := TxConfig{Vault: p, Primary: p, Policy: p, Sessions: p, Issuer: "AMOS", Now: func() time.Time { panic("constructor called clock") }}
	if got, err := NewWithTxRunner(mfaValueRunner{}, cfg); got == nil || err != nil {
		t.Fatal("valid construction failed")
	}
	if got, err := New(Config{&storage.DB{}, p, p, p, p, "AMOS", cfg.Now}); got == nil || err != nil {
		t.Fatal("legacy construction failed")
	}
	if got, err := NewWithTxRunner((*mfaPointerRunner)(nil), cfg); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("typed-nil construction accepted")
	}
}
