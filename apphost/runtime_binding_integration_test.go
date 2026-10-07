package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// These profiles are operator-precreated. Runtime tests never initialize binding
// or schema, and never receive a provisioning credential.
func bindingRuntimeDB(t *testing.T, variable string) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		t.Fatalf("required TLS runtime profile absent: set %s", variable)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required runtime configuration unavailable")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime configuration close failed")
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
		t.Fatal("required runtime configuration invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("required runtime configuration has trailing data")
	}
	roots, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		t.Fatal("required runtime trust roots unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required runtime TLS connection failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime handle close failed")
		}
	})
	return db
}

func TestRuntimeBindingRequiredService(t *testing.T) {
	bound := bindingRuntimeDB(t, "AMOS_BINDING_BOUND_RUNTIME_TEST_CONFIG")
	empty := bindingRuntimeDB(t, "AMOS_BINDING_EMPTY_RUNTIME_TEST_CONFIG")
	missing := bindingRuntimeDB(t, "AMOS_BINDING_MISSING_RUNTIME_TEST_CONFIG")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	check := func(runner storage.TxRunner, ids [3]uuid.UUID) error {
		return runtimeBindingReady(ctx, runner, ids[0], ids[1], ids[2])
	}
	t.Run("bound read only repeat", func(t *testing.T) {
		calls := 0
		runner := bindingRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
			return bound.WithTx(ctx, opts, func(tx *sql.Tx) error {
				var readOnly, isolation string
				if err := tx.QueryRowContext(ctx, `SHOW transaction_read_only`).Scan(&readOnly); err != nil {
					return err
				}
				if err := tx.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
					return err
				}
				if readOnly != "on" || isolation != "read committed" {
					t.Fatal("actual transaction options differ")
				}
				calls++
				return fn(tx)
			})
		})
		for range 2 {
			if err := check(runner, bindingRealm); err != nil {
				t.Fatal("bound realm not ready")
			}
		}
		if calls != 2 {
			t.Fatal("wrong transaction count")
		}
	})
	for i, name := range []string{"installation", "application", "environment"} {
		t.Run("mismatch "+name, func(t *testing.T) {
			ids := bindingRealm
			ids[i] = uuid.MustParse("01900000-0000-7000-8000-000000000004")
			if err := check(bound, ids); err != errRuntimeBindingConfiguration {
				t.Fatal("mismatching realm accepted or misclassified")
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		if err := check(empty, bindingRealm); err != errRuntimeBindingConfiguration {
			t.Fatal("missing binding accepted or misclassified")
		}
	})
	t.Run("missing schema", func(t *testing.T) {
		if err := check(missing, bindingRealm); err != errRuntimeBindingUnavailable {
			t.Fatal("missing schema accepted or leaked")
		}
	})
	t.Run("failed completion after successful read", func(t *testing.T) {
		callbackSucceeded, realRollback, failedCommit := false, false, false
		runner := bindingRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
			err := bound.WithTx(ctx, opts, func(tx *sql.Tx) error {
				if err := fn(tx); err != nil {
					return err
				}
				callbackSucceeded = true
				if err := tx.Rollback(); err != nil {
					return err
				}
				realRollback = true
				return nil // RuntimeDB must try and fail to commit this transaction.
			})
			failedCommit = errors.Is(err, storage.ErrTransaction)
			return err
		})
		if err := check(runner, bindingRealm); err != errRuntimeBindingUnavailable {
			t.Fatal("failed transaction exposed successful binding readiness")
		}
		if !callbackSucceeded || !realRollback || !failedCommit || ctx.Err() != nil {
			t.Fatal("completion failure did not follow a real successful read and rollback under live context")
		}
	})
	t.Run("joined cleanup error classification", func(t *testing.T) {
		callbackMismatch := false
		runner := bindingRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
			err := empty.WithTx(ctx, opts, fn)
			callbackMismatch = err == errRuntimeBindingConfiguration
			// This is a synthetic cleanup-error classification check, after a real
			// empty-row read. It does not claim an actual PostgreSQL rollback failure.
			return errors.Join(err, errors.New("PRIVATE CLEANUP DETAIL"))
		})
		if err := check(runner, bindingRealm); err != errRuntimeBindingUnavailable || !callbackMismatch {
			t.Fatal("cleanup error hidden by configuration classification")
		}
	})
	t.Run("canceled", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		err := runtimeBindingReady(canceled, bound, bindingRealm[0], bindingRealm[1], bindingRealm[2])
		if !errors.Is(err, errRuntimeBindingUnavailable) || !errors.Is(err, context.Canceled) {
			t.Fatal("canceled runtime operation lost safe cause")
		}
	})
	t.Run("closed", func(t *testing.T) {
		if err := missing.Close(); err != nil {
			t.Fatal("runtime handle close failed")
		}
		if err := check(missing, bindingRealm); err != errRuntimeBindingUnavailable {
			t.Fatal("closed runtime handle accepted")
		}
	})
	if err := check(bound, bindingRealm); err != nil {
		t.Fatal("readiness checks changed the immutable binding")
	}
}
