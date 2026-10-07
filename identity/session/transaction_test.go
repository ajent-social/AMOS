package session_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	_ func(*storage.DB, session.Config) (*session.Service, error)      = session.New
	_ func(session.TxRunner, session.Config) (*session.Service, error) = session.NewWithTxRunner
	_ session.TxRunner                                                 = (*storage.DB)(nil)
	_ session.TxRunner                                                 = (*storage.RuntimeDB)(nil)
)

type sessionRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f sessionRunnerFunc) WithTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(ctx, opts, fn)
}

type sessionNilPointer struct{}

func (*sessionNilPointer) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor invoked dependency")
}

type sessionNilMap map[string]int

func (sessionNilMap) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor invoked dependency")
}

type sessionNilSlice []int

func (sessionNilSlice) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor invoked dependency")
}

type sessionNilChannel chan int

func (sessionNilChannel) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor invoked dependency")
}

type sessionValueRunner struct{}

func (sessionValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor invoked dependency")
}

func sessionConfig(t *testing.T) session.Config {
	t.Helper()
	return session.Config{InstallationID: id(t), ApplicationID: id(t), EnvironmentID: id(t), AllowedOrigins: []string{"https://session.example.test"}, CookieSecure: true}
}

func TestSessionTxRunnerNilAdmission(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   session.TxRunner
	}{
		{"interface", nil}, {"pointer", (*sessionNilPointer)(nil)}, {"map", sessionNilMap(nil)}, {"slice", sessionNilSlice(nil)}, {"channel", sessionNilChannel(nil)}, {"function", sessionRunnerFunc(nil)}, {"legacy handle", (*storage.DB)(nil)}, {"runtime handle", (*storage.RuntimeDB)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := session.NewWithTxRunner(tc.db, sessionConfig(t))
			if got != nil || !errors.Is(err, session.ErrInvalidConfiguration) {
				t.Fatalf("nil runner admitted: service=%t error=%v", got != nil, err)
			}
		})
	}
	if got, err := session.New(nil, sessionConfig(t)); got != nil || !errors.Is(err, session.ErrInvalidConfiguration) {
		t.Fatal("legacy nil accepted")
	}
}

func TestSessionTxRunnerCompatibilityAndValidation(t *testing.T) {
	// An unkeyed literal and reflected field inventory pin the legacy Config shape.
	cfg := session.Config{id(t), id(t), id(t), []string{"https://session.example.test"}, false, true, false}
	names := []string{"InstallationID", "ApplicationID", "EnvironmentID", "AllowedOrigins", "DevelopmentLoopback", "CookieSecure", "PersistAssurance"}
	typ := reflect.TypeOf(cfg)
	if typ.NumField() != len(names) {
		t.Fatal("Config field count changed")
	}
	for i, name := range names {
		if typ.Field(i).Name != name {
			t.Fatal("Config field order changed")
		}
	}
	for _, runner := range []session.TxRunner{sessionValueRunner{}, sessionNilMap{}, sessionNilSlice{}, make(sessionNilChannel), &storage.DB{}, &storage.RuntimeDB{}} {
		if _, err := session.NewWithTxRunner(runner, cfg); err != nil {
			t.Fatalf("non-nil runner rejected: %v", err)
		}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*session.Config)
		valid bool
	}{
		{"production", func(*session.Config) {}, true},
		{"missing installation", func(c *session.Config) { c.InstallationID = uuid.Nil }, false},
		{"missing application", func(c *session.Config) { c.ApplicationID = uuid.Nil }, false},
		{"missing environment", func(c *session.Config) { c.EnvironmentID = uuid.Nil }, false},
		{"no origins", func(c *session.Config) { c.AllowedOrigins = nil }, false},
		{"insecure production", func(c *session.Config) { c.CookieSecure = false }, false},
		{"http production", func(c *session.Config) { c.AllowedOrigins = []string{"http://session.example.test"} }, false},
		{"development", func(c *session.Config) {
			c.DevelopmentLoopback = true
			c.CookieSecure = false
			c.AllowedOrigins = []string{"http://127.0.0.1:8080"}
		}, true},
		{"secure development", func(c *session.Config) {
			c.DevelopmentLoopback = true
			c.AllowedOrigins = []string{"http://127.0.0.1:8080"}
		}, false},
		{"remote development", func(c *session.Config) { c.DevelopmentLoopback = true; c.CookieSecure = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := cfg
			tc.edit(&candidate)
			legacy, oldErr := session.New(&storage.DB{}, candidate)
			fresh, newErr := session.NewWithTxRunner(sessionValueRunner{}, candidate)
			if (oldErr == nil) != tc.valid || (newErr == nil) != tc.valid || (legacy != nil) != tc.valid || (fresh != nil) != tc.valid {
				t.Fatal("constructor validation differs")
			}
			if !tc.valid && (!errors.Is(oldErr, session.ErrInvalidConfiguration) || !errors.Is(newErr, session.ErrInvalidConfiguration)) {
				t.Fatal("configuration sentinel changed")
			}
		})
	}
}

func TestSessionTxRunnerFailureMapping(t *testing.T) {
	calls := 0
	failure := errors.New("synthetic transaction failure")
	runner := sessionRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, _ func(*sql.Tx) error) error {
		calls++
		if ctx == nil || opts != nil {
			t.Error("transaction context/options changed")
		}
		return failure
	})
	svc, err := session.NewWithTxRunner(runner, sessionConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("construction performed I/O")
	}
	issued, err := svc.Issue(context.Background(), authproof.VerifiedCredential{}, "")
	if !errors.Is(err, session.ErrUnavailable) || issued.Cookie != nil || issued.CSRFToken != "" || !issued.AssuranceExpires.IsZero() {
		t.Fatal("transaction failure exposed credentials")
	}
	if calls != 1 {
		t.Fatal("issue did not own one transaction")
	}
	req := httptest.NewRequest(http.MethodGet, "https://session.example.test", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	rec := httptest.NewRecorder()
	svc.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unavailable authority admitted request") })).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || calls != 2 {
		t.Fatal("middleware failure mapping changed")
	}
}
