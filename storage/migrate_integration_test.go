package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
)

func TestMigrateIsIdempotentAndRecordsChecksum(t *testing.T) {
	db := newTestDB(t)
	registry := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "create_values",
		SQL:      "CREATE TABLE migration_values (value text PRIMARY KEY); INSERT INTO migration_values(value) VALUES ('one');",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := Migrate(ctx, db, registry); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := Migrate(ctx, db, registry); err != nil {
		t.Fatalf("repeated Migrate() error = %v", err)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM migration_values`); got != 1 {
		t.Fatalf("migration_values rows = %d, want 1", got)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM amos_schema_migrations`); got != 1 {
		t.Fatalf("ledger rows = %d, want 1", got)
	}
	var checksum []byte
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT checksum FROM amos_schema_migrations WHERE sequence = 1`).Scan(&checksum)
	}); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	want := sha256.Sum256([]byte(registry.Migrations()[0].SQL))
	if string(checksum) != string(want[:]) {
		t.Fatal("ledger checksum differs from SHA-256 of exact migration source")
	}
}

func TestMigrateRejectsChangedChecksumAndPreservesData(t *testing.T) {
	db := newTestDB(t)
	initial := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "create_values",
		SQL:      "CREATE TABLE migration_values (value text PRIMARY KEY); INSERT INTO migration_values(value) VALUES ('preserved');",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Migrate(ctx, db, initial); err != nil {
		t.Fatalf("initial Migrate() error = %v", err)
	}
	changed := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "create_values",
		SQL:      "CREATE TABLE migration_values (value text PRIMARY KEY); INSERT INTO migration_values(value) VALUES ('changed');",
	}}})
	if err := Migrate(ctx, db, changed); !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("changed Migrate() error = %v, want ErrMigrationChecksum", err)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM migration_values WHERE value = 'preserved'`); got != 1 {
		t.Fatalf("preserved rows = %d, want 1", got)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM migration_values WHERE value = 'changed'`); got != 0 {
		t.Fatalf("changed rows = %d, want 0", got)
	}
}

func TestMigrateConcurrentCallsApplyEachMigrationOnce(t *testing.T) {
	db := newTestDB(t)
	registry := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{
		{Sequence: 1, Name: "create_values", SQL: "CREATE TABLE migration_values (value text PRIMARY KEY); INSERT INTO migration_values(value) VALUES ('one');"},
		{Sequence: 2, Name: "insert_second", SQL: "INSERT INTO migration_values(value) VALUES ('two');"},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wait sync.WaitGroup
	errCh := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errCh <- Migrate(ctx, db, registry)
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Migrate() error = %v", err)
		}
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM migration_values`); got != 2 {
		t.Fatalf("migration_values rows = %d, want 2", got)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM amos_schema_migrations`); got != 2 {
		t.Fatalf("ledger rows = %d, want 2", got)
	}
}

func TestMigrateRollsBackSQLAndLedgerOnFailure(t *testing.T) {
	db := newTestDB(t)
	registry := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "fail_after_create",
		SQL:      "DO $$ BEGIN CREATE TABLE should_rollback (id integer); RAISE EXCEPTION 'expected failure'; END $$;",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Migrate(ctx, db, registry); !errors.Is(err, ErrMigrationApply) {
		t.Fatalf("Migrate() error = %v, want ErrMigrationApply", err)
	}
	var relation sql.NullString
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT to_regclass('should_rollback')::text`).Scan(&relation)
	}); err != nil {
		t.Fatalf("check rollback relation: %v", err)
	}
	if relation.Valid {
		t.Fatalf("failed migration left relation %q behind", relation.String)
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM amos_schema_migrations`); got != 0 {
		t.Fatalf("ledger rows after rollback = %d, want 0", got)
	}
}

func TestMigrateRejectsAppliedHistoryOutsideRegistry(t *testing.T) {
	db := newTestDB(t)
	full := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "create_values",
		SQL:      "CREATE TABLE migration_values (value text PRIMARY KEY);",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Migrate(ctx, db, full); err != nil {
		t.Fatalf("initial Migrate() error = %v", err)
	}
	empty, err := migrations.NewRegistry()
	if err != nil {
		t.Fatalf("empty NewRegistry() error = %v", err)
	}
	if err := Migrate(ctx, db, empty); !errors.Is(err, ErrMigrationHistory) {
		t.Fatalf("older registry Migrate() error = %v, want ErrMigrationHistory", err)
	}
}

func TestMigrationLedgerRejectsMutation(t *testing.T) {
	db := newTestDB(t)
	registry := mustRegistry(t, migrations.Fragment{Namespace: "test", Migrations: []migrations.Migration{{
		Sequence: 1,
		Name:     "create_values",
		SQL:      "CREATE TABLE migration_values (value text PRIMARY KEY);",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Migrate(ctx, db, registry); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, statement := range []string{
		`UPDATE amos_schema_migrations SET migration_id = 'changed' WHERE sequence = 1`,
		`DELETE FROM amos_schema_migrations WHERE sequence = 1`,
		`TRUNCATE amos_schema_migrations`,
	} {
		t.Run(statement[:6], func(t *testing.T) {
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, statement)
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("ledger mutation error = %v, want append-only trigger rejection", err)
			}
		})
	}
	if got := scalarInt(t, db, `SELECT count(*) FROM amos_schema_migrations`); got != 1 {
		t.Fatalf("ledger rows after rejected mutations = %d, want 1", got)
	}
}

func TestWithTxRollsBackCallbackFailure(t *testing.T) {
	db := newTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	callbackErr := errors.New("reject transaction")
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE callback_rollback (id integer)`); err != nil {
			return err
		}
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("WithTx() error = %v, want callback error", err)
	}
	var relation sql.NullString
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT to_regclass('callback_rollback')::text`).Scan(&relation)
	}); err != nil {
		t.Fatalf("check callback rollback: %v", err)
	}
	if relation.Valid {
		t.Fatalf("callback failure left relation %q behind", relation.String)
	}
}

func TestWithTxHonorsCanceledContextAndClose(t *testing.T) {
	db := newTestDB(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.WithTx(canceled, nil, func(*sql.Tx) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("WithTx() error = %v, want context.Canceled", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := db.WithTx(context.Background(), nil, func(*sql.Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("WithTx() after Close error = %v, want ErrClosed", err)
	}
}

func newTestDB(t *testing.T) *DB {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse validated test database URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := Open(ctx, parsed.String())
	cancel()
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close storage database: %v", err)
		}
	})
	return db
}

func mustRegistry(t *testing.T, fragments ...migrations.Fragment) migrations.Registry {
	t.Helper()
	registry, err := migrations.NewRegistry(fragments...)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func scalarInt(t *testing.T, db *DB, query string) int {
	t.Helper()
	var value int
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query).Scan(&value)
	}); err != nil {
		t.Fatalf("query scalar: %v", err)
	}
	return value
}
