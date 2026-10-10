package email

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	_ func(*storage.DB, *sqlstore.Store, *deliveryemail.Renderer, ProtectedMaterialWriter, Config) (*Service, error) = New
	_ func(TxRunner, *sqlstore.Store, *deliveryemail.Renderer, ProtectedMaterialWriter, Config) (*Service, error)    = NewWithTxRunner
	_ TxRunner                                                                                                       = (*storage.DB)(nil)
	_ TxRunner                                                                                                       = (*storage.RuntimeDB)(nil)
	// Named legacy fields retain zero-environment construction after the reviewed W1 addition.
	_ = Config{DevelopmentLoopback: false, InstallationID: uuid.Nil, ApplicationID: uuid.Nil, ApplicationOrigin: "", ChallengeLifetime: time.Duration(0)}
)

type emailRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f emailRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type emailMapRunner map[string]int

func (emailMapRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected transaction")
}

type emailSliceRunner []int

func (emailSliceRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected transaction")
}

type emailChannelRunner chan int

func (emailChannelRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected transaction")
}

type emailValueRunner struct{}

func (emailValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected transaction")
}

type emailPointerRunner struct{}

func (*emailPointerRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected transaction")
}

type emailPanicMaterial struct{}

func (emailPanicMaterial) PutVerificationMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error {
	panic("unexpected material write")
}

func emailTxConfig(t *testing.T) Config {
	t.Helper()
	return Config{InstallationID: emailID(t), ApplicationID: emailID(t), ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime}
}

func TestEmailTxRunnerNilAdmission(t *testing.T) {
	for name, runner := range map[string]TxRunner{"nil": nil, "legacy": (*storage.DB)(nil), "runtime": (*storage.RuntimeDB)(nil), "pointer": (*emailPointerRunner)(nil), "function": emailRunnerFunc(nil), "map": emailMapRunner(nil), "slice": emailSliceRunner(nil), "channel": emailChannelRunner(nil)} {
		t.Run(name, func(t *testing.T) {
			// A zero renderer would panic if constructor admission reached its Render call.
			got, err := NewWithTxRunner(runner, &sqlstore.Store{}, &deliveryemail.Renderer{}, emailPanicMaterial{}, emailTxConfig(t))
			if got != nil || err != ErrInvalidRequest {
				t.Fatalf("nil admission = %v, %v", got, err)
			}
		})
	}
	if got, err := New(nil, nil, nil, nil, emailTxConfig(t)); got != nil || err != ErrInvalidRequest {
		t.Fatal("legacy nil admission changed")
	}
}

func TestEmailTxRunnerConstructionCompatibility(t *testing.T) {
	cfg := emailTxConfig(t)
	for _, runner := range []TxRunner{emailValueRunner{}, emailMapRunner{}, emailSliceRunner{}, make(emailChannelRunner), &emailPointerRunner{}, &storage.DB{}, &storage.RuntimeDB{}} {
		s, err := NewWithTxRunner(runner, nil, nil, nil, cfg)
		if err != nil || s == nil {
			t.Fatalf("non-nil runner rejected: %v", err)
		}
	}
	legacy, err := New(&storage.DB{}, nil, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := NewWithTxRunner(emailValueRunner{}, nil, nil, nil, cfg)
	if err != nil || modern.origin.String() != legacy.origin.String() || modern.ttl != legacy.ttl || modern.installationID != legacy.installationID || modern.applicationID != legacy.applicationID {
		t.Fatal("legacy configuration behavior changed")
	}
	for name, edit := range map[string]func(*Config){
		"installation": func(c *Config) { c.InstallationID = uuid.Nil }, "application": func(c *Config) { c.ApplicationID = uuid.Nil },
		"origin": func(c *Config) { c.ApplicationOrigin = "http://app.example.test" }, "zero lifetime": func(c *Config) { c.ChallengeLifetime = 0 },
		"short lifetime": func(c *Config) { c.ChallengeLifetime = MinChallengeLifetime - time.Second }, "long lifetime": func(c *Config) { c.ChallengeLifetime = MaxChallengeLifetime + time.Second },
		"fractional lifetime": func(c *Config) { c.ChallengeLifetime += time.Nanosecond },
	} {
		t.Run(name, func(t *testing.T) {
			c := cfg
			edit(&c)
			a, e := New(&storage.DB{}, nil, nil, nil, c)
			b, f := NewWithTxRunner(emailValueRunner{}, nil, nil, nil, c)
			if a != nil || b != nil || e != ErrInvalidRequest || f != e {
				t.Fatal("configuration rejection changed")
			}
		})
	}
	for mask := 1; mask < 7; mask++ {
		var outbox *sqlstore.Store
		var renderer *deliveryemail.Renderer
		var material ProtectedMaterialWriter
		if mask&1 != 0 {
			outbox = &sqlstore.Store{}
		}
		if mask&2 != 0 {
			renderer = &deliveryemail.Renderer{}
		}
		if mask&4 != 0 {
			material = emailPanicMaterial{}
		}
		if s, e := NewWithTxRunner(emailValueRunner{}, outbox, renderer, material, cfg); s != nil || e != ErrInvalidRequest {
			t.Fatal("partial delivery configuration accepted")
		}
	}
	cfg.DevelopmentLoopback = true
	cfg.ApplicationOrigin = "http://127.0.0.1:8080"
	if _, e := NewWithTxRunner(emailValueRunner{}, nil, nil, nil, cfg); e != nil {
		t.Fatal("development origin rejected")
	}
}

func TestEmailTxRunnerOptionsAndErrors(t *testing.T) {
	cfg := emailTxConfig(t)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "sentinel")
	calls := 0
	var options *sql.TxOptions
	failure := errors.New("private runner failure")
	runner := emailRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		calls++
		if c != ctx || fn == nil {
			t.Fatal("context/callback changed")
		}
		options = o
		return failure
	})
	outbox, e := sqlstore.NewWithTx(runner, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	renderer, e := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: cfg.ApplicationOrigin, MaxBodyBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewWithTxRunner(runner, outbox, renderer, emailPanicMaterial{}, cfg)
	if e != nil || calls != 0 {
		t.Fatal("constructor performed transaction")
	}
	token := strings.Repeat("A", 43)
	if p, e := s.Preview(ctx, emailID(t), token); p.Available || e != ErrUnavailable || calls != 1 || options == nil || !options.ReadOnly || options.Isolation != sql.LevelDefault {
		t.Fatal("preview options or error mapping changed")
	}
	if e := s.Confirm(ctx, emailID(t), token); e != ErrUnavailable || calls != 2 || options == nil || options.ReadOnly || options.Isolation != sql.LevelReadCommitted {
		t.Fatal("confirm options or error mapping changed")
	}
	if a, e := s.IssueVerification(ctx, emailID(t), emailID(t), emailID(t)); a.Received || e != ErrUnavailable || calls != 3 || options != nil {
		t.Fatal("issue options or error mapping changed")
	}
	if p, e := s.Preview(ctx, emailID(t), "bad"); p.Available || e != nil {
		t.Fatal("invalid preview changed")
	}
	if e := s.Confirm(ctx, emailID(t), "bad"); e != ErrChallengeUnavailable {
		t.Fatal("invalid confirmation changed")
	}
	if _, e := s.IssueVerification(ctx, uuid.Nil, emailID(t), emailID(t)); e != ErrInvalidRequest {
		t.Fatal("invalid issue changed")
	}
	if calls != 3 {
		t.Fatal("invalid input invoked runner")
	}
	unavailable, e := NewWithTxRunner(runner, nil, nil, nil, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := unavailable.IssueVerification(ctx, emailID(t), emailID(t), emailID(t)); e != ErrUnavailable || calls != 3 {
		t.Fatal("missing delivery invoked runner")
	}
	request := httptest.NewRequest("POST", "/verify-email", nil)
	request.Header.Set("Origin", "https://wrong.example.test")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 403 || calls != 3 {
		t.Fatal("origin rejection changed")
	}
	failure = ErrChallengeUnavailable
	if e := s.Confirm(ctx, emailID(t), token); e != ErrChallengeUnavailable {
		t.Fatal("challenge sentinel mapping changed")
	}
}
