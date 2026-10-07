package login

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

	"github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/password"
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

type loginRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f loginRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type loginPointerRunner struct{}

func (*loginPointerRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type loginMapRunner map[string]int

func (loginMapRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type loginSliceRunner []int

func (loginSliceRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type loginChannelRunner chan int

func (loginChannelRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type loginValueRunner struct{}

func (loginValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

func loginTxConfig(t *testing.T) TxConfig {
	t.Helper()
	// Uninitialized concrete dependencies admit construction but cannot perform
	// real work. Construction must neither probe them nor open a transaction.
	return TxConfig{Passwords: &password.Hasher{}, Email: &email.Service{}, Sessions: &session.Service{}, InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t)}
}
func TestLoginTxRunnerNilAdmission(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   TxRunner
	}{
		{"interface", nil}, {"pointer", (*loginPointerRunner)(nil)}, {"map", loginMapRunner(nil)},
		{"slice", loginSliceRunner(nil)}, {"channel", loginChannelRunner(nil)}, {"function", loginRunnerFunc(nil)},
		{"legacy", (*storage.DB)(nil)}, {"runtime", (*storage.RuntimeDB)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, e := NewWithTxRunner(tc.db, loginTxConfig(t)); got != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatal("nil runner admitted")
			}
		})
	}
	cfg := loginTxConfig(t)
	if got, e := New(Config{nil, cfg.Passwords, cfg.Email, cfg.Sessions, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.ChallengeLifetime}); got != nil || e != ErrConfiguration {
		t.Fatal("legacy nil admitted")
	}
}
func TestLoginTxRunnerCompatibilityAndDefaults(t *testing.T) {
	cfg := loginTxConfig(t)
	legacy := Config{&storage.DB{}, cfg.Passwords, cfg.Email, cfg.Sessions, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.ChallengeLifetime}
	names := []string{"DB", "Passwords", "Email", "Sessions", "InstallationID", "ApplicationID", "EnvironmentID", "ChallengeLifetime"}
	typ, txType := reflect.TypeOf(legacy), reflect.TypeOf(cfg)
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
	for _, runner := range []TxRunner{loginValueRunner{}, &loginPointerRunner{}, loginMapRunner{}, loginSliceRunner{}, make(loginChannelRunner), loginRunnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { panic("constructor called runner") }), &storage.DB{}, &storage.RuntimeDB{}} {
		if got, e := NewWithTxRunner(runner, cfg); e != nil || got == nil {
			t.Fatal("non-nil runner rejected")
		}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*TxConfig)
		valid bool
	}{
		{"default", func(*TxConfig) {}, true},
		{"explicit lifetime", func(c *TxConfig) { c.ChallengeLifetime = DefaultChallengeLifetime }, true},
		{"passwords", func(c *TxConfig) { c.Passwords = nil }, false},
		{"email", func(c *TxConfig) { c.Email = nil }, false},
		{"sessions", func(c *TxConfig) { c.Sessions = nil }, false},
		{"installation", func(c *TxConfig) { c.InstallationID = uuid.Nil }, false},
		{"application", func(c *TxConfig) { c.ApplicationID = uuid.Nil }, false},
		{"environment", func(c *TxConfig) { c.EnvironmentID = uuid.Nil }, false},
		{"non v7", func(c *TxConfig) { c.InstallationID = uuid.MustParse("00000000-0000-4000-8000-000000000001") }, false},
		{"wrong variant", func(c *TxConfig) { c.ApplicationID = uuid.MustParse("00000000-0000-7000-0000-000000000001") }, false},
		{"negative lifetime", func(c *TxConfig) { c.ChallengeLifetime = -time.Second }, false},
		{"different lifetime", func(c *TxConfig) { c.ChallengeLifetime = time.Hour }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			tc.edit(&c)
			old, oe := New(Config{legacy.DB, c.Passwords, c.Email, c.Sessions, c.InstallationID, c.ApplicationID, c.EnvironmentID, c.ChallengeLifetime})
			fresh, ne := NewWithTxRunner(loginValueRunner{}, c)
			if (oe == nil) != tc.valid || (ne == nil) != tc.valid || (old != nil) != tc.valid || (fresh != nil) != tc.valid {
				t.Fatal("constructor validation diverged")
			}
			if !tc.valid {
				if oe != ErrConfiguration || ne != ErrConfiguration {
					t.Fatal("configuration sentinel changed")
				}
				return
			}
			want := c
			if want.ChallengeLifetime == 0 {
				want.ChallengeLifetime = DefaultChallengeLifetime
			}
			if old.cfg != want || fresh.cfg != want || old.db != legacy.DB {
				t.Fatal("configuration mapping/default changed")
			}
		})
	}
	if cfg.ChallengeLifetime != 0 {
		t.Fatal("caller configuration changed")
	}
}
func TestLoginTxRunnerOptionsAndFailClosed(t *testing.T) {
	cfg := loginTxConfig(t)
	hasher, e := password.New(list{}, budget{}, 1)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Passwords = hasher
	sessions, e := session.NewWithTxRunner(loginValueRunner{}, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true})
	if e != nil {
		t.Fatal(e)
	}
	cfg.Sessions = sessions
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	runner := loginRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		calls++
		if c != ctx || o != nil || fn == nil {
			t.Fatal("transaction context/options/callback changed")
		}
		return errors.New("private database diagnostic")
	})
	svc, e := NewWithTxRunner(runner, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal("constructor performed I/O")
	}
	for _, path := range []string{"/signup", "/auth"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"person@example.test","password":"a sufficiently long password"}`)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://app.example.test")
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Set-Cookie") != "" || !strings.Contains(w.Body.String(), `"code":"dependency.unavailable"`) || strings.Contains(w.Body.String(), "private database diagnostic") || strings.Contains(w.Body.String(), "csrf_token") {
			t.Fatal("transaction failure did not fail closed")
		}
	}
	if calls != 2 {
		t.Fatalf("runner calls=%d want 2", calls)
	}
}
