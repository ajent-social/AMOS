package protection

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type protectionRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f protectionRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type protectionNilMap map[string]int
type protectionNilSlice []int
type protectionNilChan chan int
type protectionValue struct{ calls *int }

func (protectionNilMap) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil map called")
}
func (protectionNilSlice) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil slice called")
}
func (protectionNilChan) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil channel called")
}
func (v protectionValue) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	*v.calls++
	return errors.New("unavailable")
}
func protectionConfig(t *testing.T) TxConfig {
	t.Helper()
	id := func() uuid.UUID {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	return TxConfig{id(), id(), id(), bytes.Repeat([]byte{17}, 32), time.Minute, 7, 7}
}

func TestProtectionTxRunnerNilAdmission(t *testing.T) {
	for name, runner := range map[string]TxRunner{
		"nil": nil, "storage": (*storage.DB)(nil), "runtime": (*storage.RuntimeDB)(nil),
		"pointer": (*protectionValue)(nil), "map": protectionNilMap(nil), "slice": protectionNilSlice(nil), "channel": protectionNilChan(nil), "function": protectionRunnerFunc(nil),
	} {
		t.Run(name, func(t *testing.T) {
			l, e := NewWithTxRunner(runner, protectionConfig(t))
			if l != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatalf("nil admission: %v %v", l, e)
			}
		})
	}
}

func TestProtectionTxRunnerConstructionAndCompatibility(t *testing.T) {
	type legacyConstructor func(Config) (*Limiter, error)
	var legacy legacyConstructor = New
	type transactionConstructor func(TxRunner, TxConfig) (*Limiter, error)
	var modern transactionConstructor = NewWithTxRunner
	_ = modern
	cfg := protectionConfig(t)
	// Positional literal deliberately freezes the legacy field types and order.
	old := Config{&storage.DB{}, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.Key, cfg.Window, cfg.IPLimit, cfg.AccountLimit}
	l, e := legacy(old)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(l.cfg, cfg) {
		t.Fatal("legacy configuration mapping changed")
	}
	for _, runner := range []TxRunner{&storage.DB{}, &storage.RuntimeDB{}} {
		if _, e := NewWithTxRunner(runner, cfg); e != nil {
			t.Fatal("constructor probed uninitialized handle", e)
		}
	}
	calls := 0
	l, e = NewWithTxRunner(protectionValue{&calls}, cfg)
	if e != nil || calls != 0 {
		t.Fatal("value runner rejected or invoked", e, calls)
	}
	before := l.digest("account", "test@example.test")
	cfg.Key[0] ^= 255
	if !bytes.Equal(before, l.digest("account", "test@example.test")) {
		t.Fatal("caller changed retained key")
	}
	if _, e = l.Allow(context.Background(), Signin, "192.0.2.1", "a@example.test"); !errors.Is(e, ErrUnavailable) || calls != 1 {
		t.Fatal("runner failure mapping", e, calls)
	}
	if _, e = l.PruneExpired(context.Background(), 1); !errors.Is(e, ErrUnavailable) || calls != 2 {
		t.Fatal("prune failure mapping", e, calls)
	}
}

func TestProtectionTxRunnerValidationAndOptions(t *testing.T) {
	calls := 0
	runner := protectionRunnerFunc(func(_ context.Context, o *sql.TxOptions, _ func(*sql.Tx) error) error {
		calls++
		if o != nil {
			t.Error("transaction options changed")
		}
		return errors.New("unavailable")
	})
	for name, mutate := range map[string]func(*TxConfig){
		"installation": func(c *TxConfig) { c.InstallationID = uuid.Nil }, "application": func(c *TxConfig) { c.ApplicationID = uuid.Nil }, "environment": func(c *TxConfig) { c.EnvironmentID = uuid.Nil },
		"short key": func(c *TxConfig) { c.Key = make([]byte, 31) }, "long key": func(c *TxConfig) { c.Key = make([]byte, 129) },
		"short window": func(c *TxConfig) { c.Window = 0 }, "long window": func(c *TxConfig) { c.Window = time.Hour + time.Second }, "fractional window": func(c *TxConfig) { c.Window = time.Second + 1 },
		"zero ip": func(c *TxConfig) { c.IPLimit = 0 }, "large ip": func(c *TxConfig) { c.IPLimit = 10001 }, "zero account": func(c *TxConfig) { c.AccountLimit = 0 }, "large account": func(c *TxConfig) { c.AccountLimit = 10001 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := protectionConfig(t)
			mutate(&cfg)
			l, e := NewWithTxRunner(runner, cfg)
			if l != nil || !errors.Is(e, ErrConfiguration) || calls != 0 {
				t.Fatal("invalid config admitted or runner invoked", e, calls)
			}
		})
	}
	l, e := NewWithTxRunner(runner, protectionConfig(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = l.Allow(context.Background(), Signin, "192.0.2.1", "x"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = l.PruneExpired(context.Background(), 1); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal("transaction count", calls)
	}
}
