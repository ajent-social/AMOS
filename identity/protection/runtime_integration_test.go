package protection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// This suite requires an operator-precreated identity_auth_limits schema and a
// runtime-only TLS connection. It never creates schema or reads admin credentials.
func protectionRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_SERVICES_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_SERVICES_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required runtime fixture config cannot be opened")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime fixture config close failed")
		}
	}()
	var cfg struct {
		Host           string `json:"host"`
		Port           uint16 `json:"port"`
		Database       string `json:"database"`
		User           string `json:"user"`
		Password       string `json:"password"`
		CAPath         string `json:"ca_path"`
		WrongHost      string `json:"wrong_host"`
		DMLTable       string `json:"dml_table"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatal("required runtime fixture JSON invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("required runtime fixture JSON has trailing data")
	}
	roots, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		t.Fatal("required runtime fixture CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required runtime TLS PostgreSQL connection failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime database close failed")
		}
	})
	return db
}

func TestProtectionRuntimeRequiredService(t *testing.T) {
	db := protectionRuntimeDB(t)
	replicaDB := protectionRuntimeDB(t)
	cfg := protectionConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	scope := []any{cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID}
	exec := func(q string, args ...any) {
		t.Helper()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, q, args...); return e }); err != nil {
			t.Fatal("runtime protection fixture DML failed")
		}
	}
	// Cleanup only this run's installation, including its deliberately foreign environment.
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := db.WithTx(c, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(c, `DELETE FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID)
			return e
		}); e != nil {
			t.Error("exact-owned protection cleanup failed")
		}
	})
	first, e := NewWithTxRunner(db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	replicaCfg := cfg
	replicaCfg.Key = append([]byte(nil), cfg.Key...)
	second, e := NewWithTxRunner(replicaDB, replicaCfg)
	if e != nil {
		t.Fatal(e)
	}
	// The original key can be discarded or overwritten after construction.
	cfg.Key[0] ^= 255
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l := first
			if i%2 != 0 {
				l = second
			}
			a, e := l.Allow(ctx, Signin, "192.0.2.7", "runtime@example.test")
			if e != nil {
				t.Error("runtime concurrent admission failed", e)
				return
			}
			if a.Allowed {
				admitted.Add(1)
			} else if a.RetryAfter <= 0 {
				t.Error("denied admission omitted retry hint")
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 7 {
		t.Fatalf("replicas admitted %d want 7", admitted.Load())
	}
	if a, e := second.Allow(ctx, Signin, "192.0.2.7", "attacker@example.test"); e != nil || a.Allowed {
		t.Fatal("denied peer bypassed shared budget", a, e)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, q, args...).Scan(&n) }); e != nil {
			t.Fatal("runtime counter read failed")
		}
		return n
	}
	if n := count(`SELECT count(*) FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND dimension='account'`, scope...); n != 1 {
		t.Fatal("denied IP expanded account counters", n)
	}
	for i := 0; i < 8; i++ {
		a, e := first.Allow(ctx, Recovery, "192.0.2."+strconv.Itoa(i+10), "shared@example.test")
		if e != nil || a.Allowed != (i < 7) {
			t.Fatal("account budget across peers", i, a, e)
		}
	}
	// Expire existing rows without waiting; retain a live row and a foreign scope.
	exec(`UPDATE identity_auth_limits SET window_start=transaction_timestamp()-interval '2 minutes',window_end=transaction_timestamp()-interval '1 minute' WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, scope...)
	foreignCfg := first.cfg
	foreignCfg.EnvironmentID, e = uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := NewWithTxRunner(db, foreignCfg)
	if e != nil {
		t.Fatal(e)
	}
	if a, e := foreign.Allow(ctx, Signin, "192.0.2.8", "foreign@example.test"); e != nil || !a.Allowed {
		t.Fatal(a, e)
	}
	foreignScope := []any{foreignCfg.InstallationID, foreignCfg.ApplicationID, foreignCfg.EnvironmentID}
	exec(`UPDATE identity_auth_limits SET window_start=transaction_timestamp()-interval '2 minutes',window_end=transaction_timestamp()-interval '1 minute' WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, foreignScope...)
	if a, e := first.Allow(ctx, Signup, "192.0.2.9", "live@example.test"); e != nil || !a.Allowed {
		t.Fatal(a, e)
	}
	if n, e := first.PruneExpired(ctx, 1); e != nil || n != 1 {
		t.Fatal("bounded prune", n, e)
	}
	if n, e := first.PruneExpired(ctx, 1000); e != nil || n != 10 {
		t.Fatal("remaining scoped prune", n, e)
	}
	if n := count(`SELECT count(*) FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, scope...); n != 2 {
		t.Fatal("prune removed live rows", n)
	}
	if n := count(`SELECT count(*) FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, foreignScope...); n != 2 {
		t.Fatal("prune crossed scope", n)
	}
	for _, mode := range []string{"missing table", "denied DML"} {
		t.Run(mode, func(t *testing.T) {
			runner := protectionRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				return db.WithTx(c, o, func(tx *sql.Tx) error {
					q := `SET LOCAL search_path TO pg_catalog`
					if mode == "denied DML" {
						q = `SET TRANSACTION READ ONLY`
					}
					if _, e := tx.ExecContext(c, q); e != nil {
						return e
					}
					return fn(tx)
				})
			})
			l, e := NewWithTxRunner(runner, first.cfg)
			if e != nil {
				t.Fatal(e)
			}
			if a, e := l.Allow(ctx, Signup, "192.0.2.10", "failure@example.test"); !errors.Is(e, ErrUnavailable) || a != (Admission{}) {
				t.Fatal("SQL failure admitted", a, e)
			}
			if n, e := l.PruneExpired(ctx, 1); !errors.Is(e, ErrUnavailable) || n != 0 {
				t.Fatal("prune SQL failure mapping", n, e)
			}
		})
	}
	// A successful callback whose transaction is rolled back cannot consume budget.
	rollback := errors.New("intentional rollback")
	runner := protectionRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		return db.WithTx(c, o, func(tx *sql.Tx) error {
			if e := fn(tx); e != nil {
				return e
			}
			return rollback
		})
	})
	rolled, e := NewWithTxRunner(runner, first.cfg)
	if e != nil {
		t.Fatal(e)
	}
	if a, e := rolled.Allow(ctx, Verification, "192.0.2.11", "rollback@example.test"); !errors.Is(e, ErrUnavailable) || a != (Admission{}) {
		t.Fatal("rollback exposed admission", a, e)
	}
	if n := count(`SELECT count(*) FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND operation='verification'`, scope...); n != 0 {
		t.Fatal("rollback retained counters", n)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if a, e := first.Allow(canceled, Verification, "192.0.2.11", "cancel@example.test"); !errors.Is(e, ErrUnavailable) || a != (Admission{}) {
		t.Fatal("cancellation exposed admission", a, e)
	}
}
