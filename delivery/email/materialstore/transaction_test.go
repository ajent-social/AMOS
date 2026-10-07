package materialstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var _ func(Config) (*Store, error) = New
var _ func(TxRunner, TxConfig) (*Store, error) = NewWithTxRunner
var _ TxRunner = (*storage.DB)(nil)
var _ TxRunner = (*storage.RuntimeDB)(nil)

// Unkeyed literals deliberately protect the legacy field types and order.
var _ = Config{false, (*storage.DB)(nil), uuid.Nil, uuid.Nil, uuid.Nil, "", map[string][]byte{}, ""}
var _ = TxConfig{false, uuid.Nil, uuid.Nil, uuid.Nil, "", map[string][]byte{}, ""}

type materialRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f materialRunnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type materialNilMap map[string]int
type materialNilSlice []int
type materialNilChan chan int
type materialPointer struct{}
type materialValue struct{}

func (materialNilMap) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil map called")
}
func (materialNilSlice) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil slice called")
}
func (materialNilChan) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil channel called")
}
func (*materialPointer) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("pointer called")
}
func (materialValue) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("value called")
}

func materialTxConfig(t *testing.T) TxConfig {
	t.Helper()
	id := func() uuid.UUID {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	return TxConfig{InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), ActiveKeyID: "v1", Keys: map[string][]byte{"v1": bytes.Repeat([]byte{17}, 32)}, ApplicationOrigin: "https://app.example.test"}
}

func TestMaterialTxNilAdmission(t *testing.T) {
	cfg := materialTxConfig(t)
	for name, db := range map[string]TxRunner{"nil": nil, "pointer": (*materialPointer)(nil), "map": materialNilMap(nil), "slice": materialNilSlice(nil), "channel": materialNilChan(nil), "function": materialRunnerFunc(nil), "development": (*storage.DB)(nil), "runtime": (*storage.RuntimeDB)(nil)} {
		t.Run(name, func(t *testing.T) {
			s, e := NewWithTxRunner(db, cfg)
			if s != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatal("nil runner admitted", e)
			}
		})
	}
	if s, e := New(Config{}); s != nil || !errors.Is(e, ErrConfiguration) {
		t.Fatal("legacy nil admitted")
	}
}

func TestMaterialTxConstructionAndCompatibility(t *testing.T) {
	cfg := materialTxConfig(t)
	for _, db := range []TxRunner{materialValue{}, &materialPointer{}, materialNilMap{}, materialNilSlice{}, make(materialNilChan), materialRunnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
		t.Fatal("constructor performed I/O")
		return nil
	}), &storage.DB{}, &storage.RuntimeDB{}} {
		if _, e := NewWithTxRunner(db, cfg); e != nil {
			t.Fatal(e)
		}
	}
	legacy := Config{cfg.DevelopmentLoopback, &storage.DB{}, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, cfg.ActiveKeyID, cfg.Keys, cfg.ApplicationOrigin}
	s, e := New(legacy)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := NewWithTxRunner(legacy.DB, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if s.installation != tx.installation || s.application != tx.application || s.environment != tx.environment || s.active != tx.active || s.origin != tx.origin {
		t.Fatal("legacy mapping changed")
	}
	a, b := reflect.TypeOf(Config{}), reflect.TypeOf(TxConfig{})
	j := 0
	for i := 0; i < a.NumField(); i++ {
		f := a.Field(i)
		if f.Name == "DB" {
			continue
		}
		g := b.Field(j)
		if f.Name != g.Name || f.Type != g.Type {
			t.Fatal("config shape changed")
		}
		j++
	}
	if j != b.NumField() {
		t.Fatal("extra transaction config fields")
	}
}

func TestMaterialTxKeyCopy(t *testing.T) {
	cfg := materialTxConfig(t)
	original := append([]byte(nil), cfg.Keys["v1"]...)
	s, e := NewWithTxRunner(materialValue{}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Keys["v1"][0] ^= 255
	delete(cfg.Keys, "v1")
	cfg.Keys["v2"] = bytes.Repeat([]byte{18}, 32)
	block, e := aes.NewCipher(original)
	if e != nil {
		t.Fatal(e)
	}
	expected, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	id := s.installation
	expiry := time.Unix(123, 0)
	nonce := make([]byte, expected.NonceSize())
	plain := []byte("synthetic secret")
	sealed := s.keys["v1"].Seal(nil, nonce, plain, s.aad(id, "v1", expiry))
	got, e := expected.Open(nil, nonce, sealed, s.aad(id, "v1", expiry))
	if e != nil || !bytes.Equal(got, plain) || len(s.keys) != 1 || s.keys["v2"] != nil {
		t.Fatal("caller key mutation changed encryption")
	}
}

func TestMaterialTxValidationAndFailureMapping(t *testing.T) {
	for name, change := range map[string]func(*TxConfig){"scope": func(c *TxConfig) { c.EnvironmentID = uuid.Nil }, "active": func(c *TxConfig) { c.ActiveKeyID = "absent" }, "key": func(c *TxConfig) { c.Keys["v1"] = make([]byte, 31) }, "origin": func(c *TxConfig) { c.ApplicationOrigin = "http://app.example.test" }} {
		t.Run(name, func(t *testing.T) {
			cfg := materialTxConfig(t)
			change(&cfg)
			if s, e := NewWithTxRunner(materialValue{}, cfg); s != nil || e != ErrConfiguration {
				t.Fatal("invalid config admitted")
			}
		})
	}
	sentinel := errors.New("private database failure")
	calls := 0
	runner := materialRunnerFunc(func(_ context.Context, o *sql.TxOptions, _ func(*sql.Tx) error) error {
		calls++
		if calls == 1 && (o == nil || !o.ReadOnly) {
			t.Fatal("resolve options changed")
		}
		if calls == 2 && o != nil {
			t.Fatal("prune options changed")
		}
		return sentinel
	})
	s, e := NewWithTxRunner(runner, materialTxConfig(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveDelivery(context.Background(), reference(t)); e != ErrUnavailable {
		t.Fatal("resolve failure leaked")
	}
	if n, e := s.PruneExpired(context.Background(), 1); n != 0 || e != ErrUnavailable {
		t.Fatal("prune failure leaked")
	}
	if calls != 2 {
		t.Fatal("transaction boundary changed")
	}
}
