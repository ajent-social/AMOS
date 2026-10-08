package store

import (
	"context"
	"database/sql"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWriterPureZeroCapabilities(t *testing.T) {
	for _, a := range []*aw.Attempt{nil, {}} {
		if _, err := NewWriter(a); err == nil {
			t.Fatal("zero writer admitted")
		}
		if _, err := NewIssuer(a, wp.Issuance{}); err == nil {
			t.Fatal("zero issuer admitted")
		}
		if (PendingRegistration{}).InAttempt(a) {
			t.Fatal("zero registration admitted")
		}
	}
}
func TestWriterPureMutatorsRejectBeforeSQL(t *testing.T) {
	s := &Store{attempt: &aw.Attempt{}}
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	digest := make([]byte, 32)
	cases := []struct {
		name string
		call func() error
	}{
		{"session", func() error { return s.CreateSession(ctx, Session{ID: id}) }},
		{"assurance", func() error { return s.SetSessionAssurance(ctx, id, "aal2", time.Now().Add(time.Minute)) }},
		{"epoch", func() error { _, err := s.AdvanceSecurityEpoch(ctx, id); return err }},
		{"challenge", func() error { return s.CreateChallenge(ctx, Challenge{ID: id}) }},
		{"consume", func() error { _, err := s.ConsumeChallenge(ctx, id, ChallengePasswordReset, digest); return err }},
		{"contact", func() error { return s.MarkEmailVerified(ctx, id, id) }},
		{"revoke", func() error { return s.RevokeSession(ctx, digest) }},
		{"scoped", func() error { return s.RevokeSessionScoped(ctx, digest, SessionScope{}) }},
		{"renew", func() error { _, err := s.FindActiveSession(ctx, digest, SessionScope{}); return err }},
		{"password", func() error { _, err := s.FindCurrentPassword(ctx, SessionScope{}, id, 0); return err }},
		{"registration", func() error { return s.CreatePendingAccount(ctx, PendingAccount{}) }},
		{"binding", func() error { return s.LinkExternalIdentity(ctx, ExternalBinding{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("zero attempt admitted")
			}
		})
	}
}

// A child process is mandatory: legacy package tests permanently select Legacy.
func TestWriterPureLegacyW1Isolation(t *testing.T) {
	if os.Getenv("AMOS_STORE_WRITER_TEST_CHILD") == "1" {
		root, err := aw.New(&storage.RuntimeDB{})
		if err != nil {
			t.Fatal(err)
		}
		if err = aw.ActivateW1(root); err != nil {
			t.Fatal(err)
		}
		if _, err = New(&sql.Tx{}); err == nil {
			t.Fatal("legacy raw transaction entered W1")
		}
		if err = aw.SelectLegacy(); err == nil {
			t.Fatal("W1 process reset to Legacy")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestWriterPureLegacyW1Isolation$", "-test.count=1")
	command.Env = append(os.Environ(), "AMOS_STORE_WRITER_TEST_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated immutable profile test: %v\n%s", err, output)
	}
}

func TestWriterPureSessionAssurancePair(t *testing.T) {
	now := time.Now().UTC()
	base := Session{AuthenticatedAt: now, ExpiresAt: now.Add(12 * time.Hour)}
	if sessionAssurance(base, false) != nil {
		t.Fatal("legacy zero pair rejected")
	}
	if sessionAssurance(base, true) == nil {
		t.Fatal("W1 missing pair admitted")
	}
	query, args := sessionInsert(base)
	if strings.Contains(query, "assurance_") || len(args) != 10 {
		t.Fatal("legacy optional columns changed")
	}
	for _, tc := range []struct {
		level  string
		expiry time.Time
		valid  bool
	}{{"aal1", base.ExpiresAt, true}, {"aal2", now.Add(15 * time.Minute), true}, {"aal3", now.Add(time.Minute), true}, {"", now.Add(time.Minute), false}, {"aal2", time.Time{}, false}, {"unknown", now.Add(time.Minute), false}, {"aal1", now.Add(time.Hour), false}, {"aal2", now, false}, {"aal2", now.Add(16 * time.Minute), false}} {
		v := base
		v.AssuranceLevel = tc.level
		v.AssuranceExpires = tc.expiry
		if (sessionAssurance(v, true) == nil) != tc.valid {
			t.Fatalf("pair %s %s", tc.level, tc.expiry)
		}
	}
	base.AssuranceLevel = "aal2"
	base.AssuranceExpires = now.Add(time.Minute)
	query, args = sessionInsert(base)
	if !strings.Contains(query, "assurance_level, assurance_expires_at") || !strings.Contains(query, "$11::timestamptz > issuance_clock.now") || len(args) != 12 {
		t.Fatal("elevated insert lacks columns or strict clock fence")
	}
}
