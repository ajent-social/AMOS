package recovery

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

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/jobs/sqlstore"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	_ func(Config) (*Service, error)             = New
	_ func(TxRunner, TxConfig) (*Service, error) = NewWithTxRunner
	_ TxRunner                                   = (*storage.DB)(nil)
	_ TxRunner                                   = (*storage.RuntimeDB)(nil)
)

type recoveryRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f recoveryRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type recoveryPointerRunner struct{}

func (*recoveryPointerRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type recoveryMapRunner map[string]int

func (recoveryMapRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type recoverySliceRunner []int

func (recoverySliceRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type recoveryChannelRunner chan int

func (recoveryChannelRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

type recoveryValueRunner struct{}

func (recoveryValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("constructor called runner")
}

func recoveryTxConfig(t *testing.T) TxConfig {
	t.Helper()
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return TxConfig{Passwords: &password.Hasher{}, Outbox: &sqlstore.Store{}, Renderer: renderer, Materials: recoveryNoMaterial{}, Policy: allowRecoveryPolicy{}, InstallationID: newRecoveryID(t), ApplicationID: newRecoveryID(t), EnvironmentID: newRecoveryID(t), ApplicationOrigin: "https://app.example.test"}
}
func TestRecoveryTxRunnerNilAdmission(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   TxRunner
	}{
		{"interface", nil}, {"pointer", (*recoveryPointerRunner)(nil)}, {"map", recoveryMapRunner(nil)},
		{"slice", recoverySliceRunner(nil)}, {"channel", recoveryChannelRunner(nil)}, {"function", recoveryRunnerFunc(nil)},
		{"legacy", (*storage.DB)(nil)}, {"runtime", (*storage.RuntimeDB)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, e := NewWithTxRunner(tc.db, recoveryTxConfig(t)); got != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatal("nil runner admitted")
			}
		})
	}
	cfg := recoveryTxConfig(t)
	if got, e := New(Config{nil, cfg.Passwords, cfg.Outbox, cfg.Renderer, cfg.Materials, cfg.Policy, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.ApplicationOrigin, cfg.DevelopmentLoopback, cfg.ChallengeLifetime}); got != nil || e != ErrConfiguration {
		t.Fatal("legacy nil admitted")
	}
}
func TestRecoveryTxRunnerCompatibilityAndDefaults(t *testing.T) {
	cfg := recoveryTxConfig(t)
	legacy := Config{&storage.DB{}, cfg.Passwords, cfg.Outbox, cfg.Renderer, cfg.Materials, cfg.Policy, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.ApplicationOrigin, cfg.DevelopmentLoopback, cfg.ChallengeLifetime}
	names := []string{"DB", "Passwords", "Outbox", "Renderer", "Materials", "Policy", "InstallationID", "ApplicationID", "EnvironmentID", "ApplicationOrigin", "DevelopmentLoopback", "ChallengeLifetime"}
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
	for _, runner := range []TxRunner{recoveryValueRunner{}, &recoveryPointerRunner{}, recoveryMapRunner{}, recoverySliceRunner{}, make(recoveryChannelRunner), recoveryRunnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { panic("constructor called runner") }), &storage.DB{}, &storage.RuntimeDB{}} {
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
		{"minimum lifetime", func(c *TxConfig) { c.ChallengeLifetime = deliveryemail.MinExpirySeconds * time.Second }, true},
		{"maximum lifetime", func(c *TxConfig) { c.ChallengeLifetime = deliveryemail.MaxExpirySeconds * time.Second }, true},
		{"over maximum", func(c *TxConfig) { c.ChallengeLifetime = (deliveryemail.MaxExpirySeconds + 1) * time.Second }, false},
		{"under minimum", func(c *TxConfig) { c.ChallengeLifetime = (deliveryemail.MinExpirySeconds - 1) * time.Second }, false},
		{"renderer origin mismatch", func(c *TxConfig) { c.ApplicationOrigin = "https://different.example.test" }, false},
		{"passwords", func(c *TxConfig) { c.Passwords = nil }, false},
		{"outbox", func(c *TxConfig) { c.Outbox = nil }, false},
		{"renderer", func(c *TxConfig) { c.Renderer = nil }, false},
		{"materials", func(c *TxConfig) { c.Materials = nil }, false},
		{"policy", func(c *TxConfig) { c.Policy = nil }, false},
		{"origin", func(c *TxConfig) { c.ApplicationOrigin = "bad" }, false},
		{"installation", func(c *TxConfig) { c.InstallationID = uuid.Nil }, false},
		{"application", func(c *TxConfig) { c.ApplicationID = uuid.Nil }, false},
		{"environment", func(c *TxConfig) { c.EnvironmentID = uuid.Nil }, false},
		{"non v7", func(c *TxConfig) { c.InstallationID = uuid.MustParse("00000000-0000-4000-8000-000000000001") }, false},
		{"wrong variant", func(c *TxConfig) { c.ApplicationID = uuid.MustParse("00000000-0000-7000-0000-000000000001") }, false},
		{"negative lifetime", func(c *TxConfig) { c.ChallengeLifetime = -time.Second }, false},
		{"fractional lifetime", func(c *TxConfig) { c.ChallengeLifetime = time.Second + time.Nanosecond }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			tc.edit(&c)
			old, oe := New(Config{legacy.DB, c.Passwords, c.Outbox, c.Renderer, c.Materials, c.Policy, c.InstallationID, c.ApplicationID, c.EnvironmentID, c.ApplicationOrigin, c.DevelopmentLoopback, c.ChallengeLifetime})
			fresh, ne := NewWithTxRunner(recoveryValueRunner{}, c)
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

// This local dependency panics if construction tries to write protected material.
type recoveryNoMaterial struct{}

func (recoveryNoMaterial) PutPasswordResetMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error {
	panic("constructor called material writer")
}

func TestRecoveryTxRunnerOptionsAndFailClosed(t *testing.T) {
	cfg := recoveryTxConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	readOnly := false
	runner := recoveryRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		calls++
		if c != ctx || fn == nil || (readOnly && (o == nil || !o.ReadOnly || o.Isolation != sql.LevelDefault)) || (!readOnly && o != nil) {
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
	r := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(`{"email":"person@example.test"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	svc.RequestHandler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"dependency.unavailable"`) || strings.Contains(w.Body.String(), "private database diagnostic") {
		t.Fatal("transaction failure did not fail closed")
	}
	if e := svc.request(ctx, "person@example.test"); e != ErrUnavailable {
		t.Fatal("request failure changed")
	}
	readOnly = true
	if ok, e := svc.preview(ctx, newRecoveryID(t), make([]byte, 32)); ok || e != ErrUnavailable {
		t.Fatal("preview failure changed")
	}
	if _, _, ok, e := svc.resetPreflight(ctx, newRecoveryID(t), make([]byte, 32)); ok || e != ErrUnavailable {
		t.Fatal("preflight failure changed")
	}
	if e := svc.change(ctx, identity.Principal{}, oldPassword, newPassword); e != ErrUnavailable {
		t.Fatal("change failure changed")
	}
	if e := svc.complete(ctx, newRecoveryID(t), make([]byte, 32), newPassword); e != ErrUnavailable {
		t.Fatal("completion failure changed")
	}
	if calls != 6 {
		t.Fatalf("runner calls=%d want 6", calls)
	}
}
