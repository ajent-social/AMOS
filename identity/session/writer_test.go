package session

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

func writerConfig(t *testing.T) Config {
	t.Helper()
	return Config{InstallationID: uuid.Must(uuid.NewV7()), ApplicationID: uuid.Must(uuid.NewV7()), EnvironmentID: uuid.Must(uuid.NewV7()), AllowedOrigins: []string{"https://example.test"}, CookieSecure: true}
}
func TestWriterMiddlewareRejectsOriginAndCSRFBeforeRenewal(t *testing.T) {
	cfg := writerConfig(t)
	s, err := NewWithWriter(&aw.Root{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cases := []struct {
		name, origin string
	}{
		{name: "denied origin", origin: "https://attacker.test"},
		{name: "missing csrf", origin: "https://example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://example.test/protected", http.NoBody)
			request.AddCookie(&http.Cookie{Name: s.cookieName, Value: token})
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			called := false
			response := httptest.NewRecorder()
			s.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status %d, want forbidden", response.Code)
			}
			if called {
				t.Fatal("rejected request reached handler")
			}
		})
	}
}

func testPrincipal(t *testing.T, cfg Config, level string, at, expiry time.Time) identity.Principal {
	t.Helper()
	proof, err := authproof.NewVerifiedCredential(uuid.Must(uuid.NewV7()), cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, 0, "email_password", at, level, expiry)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
	if !ok {
		t.Fatal("principal bridge")
	}
	return p
}
func TestWriterPureCopiedOriginsAndAdmission(t *testing.T) {
	cfg := writerConfig(t)
	s, err := NewWithWriter(&aw.Root{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AllowedOrigins[0] = "https://attacker.test"
	r := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
	r.Header.Set("Origin", "https://attacker.test")
	if s.AllowsOrigin(r) {
		t.Fatal("caller mutated service origins")
	}
	if _, err = s.AdmitWriterRequest(r); err == nil {
		t.Fatal("unbounded request admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: "a"})
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: "b"})
	if _, err = s.AdmitWriterRequest(r); err == nil {
		t.Fatal("duplicate cookie admitted")
	}
	r.Header.Del("Cookie")
	request, err := s.AdmitWriterRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.writerRequest(ctx, request); err == nil {
		t.Fatal("replaced context admitted")
	}
	if _, err = s.writerRequest(r.Context(), request); err != nil {
		t.Fatal(err)
	}
	other, err := NewWithWriter(&aw.Root{}, writerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.writerRequest(r.Context(), request); err == nil {
		t.Fatal("cross service admitted")
	}
	cancel()
	if _, err = s.writerRequest(r.Context(), request); err == nil {
		t.Fatal("canceled request admitted")
	}
}
func TestWriterPureCurrentProofAndContextActions(t *testing.T) {
	cfg := writerConfig(t)
	s, err := NewWithWriter(&aw.Root{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	p := testPrincipal(t, cfg, "aal1", now.Add(-time.Minute), now.Add(time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	proof, err := credentialForPrincipal(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx = identity.ContextWithVerifiedCredential(ctx, proof)
	if _, err = s.AdmitWriterContext(ctx, wp.MFABegin); err == nil {
		t.Fatal("public principal admitted")
	}
	if _, err = s.RecheckCurrentTx(ctx, &sql.Tx{}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("public principal recheck: %v", err)
	}
	ctx = s.admitCurrent(ctx, uuid.Must(uuid.NewV7()), p, make([]byte, 32))
	for _, action := range []wp.Action{wp.MFABegin, wp.PasswordChange} {
		request, e := s.AdmitWriterContext(ctx, action)
		if e != nil {
			t.Fatal(e)
		}
		if !s.WriterPrincipalMatches(request, p) {
			t.Fatal("exact principal did not match")
		}
		if s.WriterPrincipalMatches(request, identity.Principal{}) {
			t.Fatal("zero principal matched")
		}
		if _, e = s.StageWriter(ctx, &aw.Attempt{}, wp.Issuance{}, request); e == nil {
			t.Fatal("context-only issued")
		}
	}
	if _, err = s.AdmitWriterContext(ctx, wp.PasswordSignIn); err == nil {
		t.Fatal("context-only issuing action admitted")
	}
	other, err := NewWithWriter(&aw.Root{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.current(ctx); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("cross instance proof")
	}
	altered := testPrincipal(t, cfg, "aal1", now.Add(-time.Minute), now.Add(time.Hour))
	credential, err := credentialForPrincipal(altered)
	if err != nil {
		t.Fatal(err)
	}
	changed := identity.ContextWithVerifiedCredential(ctx, credential)
	if _, err = s.current(changed); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("mismatched snapshot admitted")
	}
	actorRequest, err := s.AdmitWriterContext(ctx, wp.MFABegin)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = s.ActorForWriter(ctx, &aw.Attempt{}, actorRequest); !errors.Is(err, ErrUnavailable) {
		t.Fatal("canceled actor misclassified", err)
	}
	if _, err = s.current(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("canceled proof admitted")
	}
}
func TestWriterPureAssuranceCaseTable(t *testing.T) {
	cfg := writerConfig(t)
	now := time.Now().UTC()
	at := now.Add(-5 * time.Minute)
	absolute := at.Add(maxAge)
	cases := []struct {
		name, admitted, current, want string
		admittedExpiry, currentExpiry time.Time
		wantExpiry                    time.Time
		denied                        bool
	}{
		{"aal1 never upgrades", "aal1", "aal3", "aal1", now.Add(time.Hour), now.Add(time.Minute), now.Add(time.Hour), false},
		{"aal1 strict expiry", "aal1", "aal1", "", now, time.Time{}, time.Time{}, true},
		{"expired admitted elevated", "aal2", "aal3", "aal1", now, now.Add(time.Minute), absolute, false},
		{"lower current", "aal3", "aal2", "aal2", now.Add(4 * time.Minute), now.Add(2 * time.Minute), now.Add(2 * time.Minute), false},
		{"higher current no upgrade", "aal2", "aal3", "aal2", now.Add(time.Minute), now.Add(3 * time.Minute), now.Add(time.Minute), false},
		{"durable base downgrade", "aal3", "aal1", "aal1", now.Add(time.Minute), time.Time{}, absolute, false},
		{"durable strict boundary", "aal2", "aal2", "aal1", now.Add(time.Minute), now, absolute, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testPrincipal(t, cfg, tc.admitted, at, tc.admittedExpiry)
			v := currentRow{absolute: absolute, level: tc.current, elevated: sql.NullTime{Time: tc.currentExpiry, Valid: tc.current != "aal1"}}
			level, expiry, err := currentAssurance(p, v, now)
			if tc.denied {
				if !errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("want denial got %v", err)
				}
				return
			}
			if err != nil || level != tc.want || !expiry.Equal(tc.wantExpiry) {
				t.Fatalf("got %s %s %v", level, expiry, err)
			}
		})
	}
}
func TestWriterPureMalformedAssurance(t *testing.T) {
	now := time.Now().UTC()
	v := currentRow{authenticated: now, absolute: now.Add(maxAge), level: "aal1"}
	if err := validDurableAssurance(v); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		level  string
		valid  bool
		expiry time.Time
	}{{"unknown", false, time.Time{}}, {"aal1", true, now.Add(time.Minute)}, {"aal2", false, time.Time{}}, {"aal3", true, now.Add(16 * time.Minute)}, {"aal2", true, now}} {
		v.level = tc.level
		v.elevated = sql.NullTime{Time: tc.expiry, Valid: tc.valid}
		if validDurableAssurance(v) == nil {
			t.Fatal("malformed durable assurance accepted")
		}
	}
}
func TestWriterPurePublicationRejectsZero(t *testing.T) {
	s, err := NewWithWriter(&aw.Root{}, writerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.PublishWriter(aw.Completion{}, wp.Permit{}, Staged{}); err == nil || got.Cookie != nil || got.CSRFToken != "" {
		t.Fatal("zero publication disclosed")
	}
}
