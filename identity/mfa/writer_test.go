package mfa

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

type writerTestVault struct{}

func (writerTestVault) Seal(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error) {
	return nil, ErrUnavailable
}
func (writerTestVault) Open(context.Context, *sql.Tx, Scope, uuid.UUID, FactorState, time.Time, string, []byte) ([]byte, error) {
	return nil, ErrUnavailable
}

type writerTestPolicy struct{}

func (writerTestPolicy) Ready(context.Context, *sql.Tx) error { return ErrUnavailable }
func (writerTestPolicy) AuthorizeMFA(context.Context, *sql.Tx, identity.Principal, string) error {
	return ErrDenied
}
func writerTestConfig() TxConfig {
	return TxConfig{Vault: writerTestVault{}, Primary: &primaryproof.Verifier{}, Sessions: &session.Service{}, Policy: writerTestPolicy{}, Issuer: "Example"}
}
func TestNativeWriterPureConstruction(t *testing.T) {
	cfg := writerTestConfig()
	root := &aw.Root{}
	service, err := NewWithWriter(root, cfg)
	if err != nil || service.root != root || service.db != nil || service.writerPrimary != cfg.Primary || service.writerSessions != cfg.Sessions {
		t.Fatal("writer did not retain exact native dependencies")
	}
	cfg.Issuer = "changed"
	if service.cfg.Issuer != "Example" {
		t.Fatal("mutable configuration retained")
	}
	cases := []struct {
		name   string
		mutate func(*TxConfig)
	}{{"clock", func(c *TxConfig) { c.Now = time.Now }}, {"nil primary", func(c *TxConfig) { c.Primary = (*primaryproof.Verifier)(nil) }}, {"nil sessions", func(c *TxConfig) { c.Sessions = (*session.Service)(nil) }}, {"no vault", func(c *TxConfig) { c.Vault = nil }}, {"typed nil vault", func(c *TxConfig) { c.Vault = (*writerTestVault)(nil) }}, {"typed nil policy", func(c *TxConfig) { c.Policy = (*writerTestPolicy)(nil) }}, {"no policy", func(c *TxConfig) { c.Policy = nil }}, {"issuer whitespace", func(c *TxConfig) { c.Issuer = " bad " }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := writerTestConfig()
			tc.mutate(&c)
			if got, err := NewWithWriter(root, c); got != nil || !errors.Is(err, ErrConfiguration) {
				t.Fatal("invalid writer configuration admitted")
			}
		})
	}
	if got, err := NewWithWriter(nil, writerTestConfig()); got != nil || err == nil {
		t.Fatal("nil root admitted")
	}
}
func TestNativeWriterPureNoPublicPrincipalAdmission(t *testing.T) {
	s, err := NewWithWriter(&aw.Root{}, writerTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := s.BeginEnrollment(ctx, identity.Principal{}, "bounded password"); got != (Enrollment{}) || !errors.Is(err, ErrDenied) {
		t.Fatal("public principal established enrollment authority")
	}
	if got, err := s.Status(ctx, identity.Principal{}); got != (FactorStatus{}) || !errors.Is(err, ErrDenied) {
		t.Fatal("public principal established status authority")
	}
	if got, err := NewWriter(&aw.Attempt{}); got != nil || err == nil {
		t.Fatal("zero attempt made factor writer")
	}
	if got, err := NewReadStore(nil); got != nil || err == nil {
		t.Fatal("nil transaction made reader")
	}
}
func TestNativeWriterPureFactorShapeAndCounterSnapshot(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	epoch := int64(2)
	expiry := now.Add(DefaultPendingLifetime)
	f := Factor{ID: id(), Scope: Scope{id(), id(), id(), id()}, SeedCiphertext: make([]byte, 32), State: FactorPending, PendingSecurityEpoch: &epoch, LastUsedStep: -1, CreatedAt: now, ExpiresAt: &expiry}
	if err := factorShape(f); err != nil {
		t.Fatal("valid pending factor rejected")
	}
	for _, mutate := range []func(*Factor){func(v *Factor) { v.LastUsedStep = 0 }, func(v *Factor) { v.ExpiresAt = nil }, func(v *Factor) { v.State = "unknown" }, func(v *Factor) { v.FailedAttempts = 6 }, func(v *Factor) { v.PendingSecurityEpoch = nil }} {
		v := f
		mutate(&v)
		if factorShape(v) == nil {
			t.Fatal("malformed stored factor admitted")
		}
	}
	check := factorCheck(f, epoch, -1, now.Add(time.Second))
	if check.AcceptedStep != -1 || !check.WindowUntil.IsZero() || !check.PendingUntil.Equal(expiry) {
		t.Fatal("bad-code snapshot invented a TOTP window")
	}
	clone := f
	clone.SeedCiphertext = append([]byte(nil), f.SeedCiphertext...)
	clone.SeedCiphertext[0] = 1
	if sameFactor(f, clone) {
		t.Fatal("factor ciphertext change not compared")
	}
	clone = f
	clone.FailedAttempts = 1
	if sameFactor(f, clone) {
		t.Fatal("factor counter change not compared")
	}
}
