// Package testkit provides isolated PostgreSQL databases for integration tests.
package testkit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const DatabaseURLEnv = "AMOS_TEST_DATABASE_URL"

// DatabaseURL reads and validates the PostgreSQL URL required by integration
// tests. It never falls back to a developer or production database setting.
func DatabaseURL() (string, error) {
	value, ok := os.LookupEnv(DatabaseURLEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required for PostgreSQL integration tests", DatabaseURLEnv)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", DatabaseURLEnv, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("%s must use the postgres or postgresql URL scheme", DatabaseURLEnv)
	}
	if parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
		return "", fmt.Errorf("%s must identify a PostgreSQL host and database", DatabaseURLEnv)
	}
	return value, nil
}

// NewPostgres creates a random schema, configures the returned connection pool
// to use it, and registers cleanup that drops only that schema. The supplied
// URL must point to a disposable test database with CREATE/DROP SCHEMA rights.
func NewPostgres(t *testing.T) (*sql.DB, string) {
	t.Helper()

	value, err := DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	adminConfig, err := pgx.ParseConfig(value)
	if err != nil {
		t.Fatalf("parse %s: %v", DatabaseURLEnv, err)
	}
	adminDB := stdlib.OpenDB(*adminConfig)
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		t.Fatalf("connect to PostgreSQL test database from %s: %v", DatabaseURLEnv, err)
	}

	schema, err := newSchemaName()
	if err != nil {
		_ = adminDB.Close()
		t.Fatalf("generate isolated PostgreSQL schema name: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteIdentifier(schema)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}

	testConfig := adminConfig.Copy()
	testConfig.RuntimeParams["search_path"] = schema
	testDB := stdlib.OpenDB(*testConfig)
	if err := testDB.PingContext(ctx); err != nil {
		_ = testDB.Close()
		cleanupErr := dropSchema(ctx, adminDB, schema)
		_ = adminDB.Close()
		if cleanupErr != nil {
			t.Fatalf("connect using isolated PostgreSQL schema: %v (schema cleanup also failed: %v)", err, cleanupErr)
		}
		t.Fatalf("connect using isolated PostgreSQL schema: %v", err)
	}

	t.Cleanup(func() {
		if err := testDB.Close(); err != nil {
			t.Errorf("close isolated PostgreSQL test connection: %v", err)
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := dropSchema(cleanupCtx, adminDB, schema); err != nil {
			t.Errorf("drop isolated PostgreSQL schema %s: %v", schema, err)
		}
		if err := adminDB.Close(); err != nil {
			t.Errorf("close PostgreSQL administrator connection: %v", err)
		}
	})
	return testDB, schema
}

func newSchemaName() (string, error) {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return "amos_test_" + hex.EncodeToString(suffix[:]), nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func dropSchema(ctx context.Context, db *sql.DB, schema string) error {
	if !strings.HasPrefix(schema, "amos_test_") || len(schema) != len("amos_test_")+32 {
		return errors.New("refusing to drop schema without the testkit-owned name shape")
	}
	_, err := db.ExecContext(ctx, "DROP SCHEMA "+quoteIdentifier(schema)+" CASCADE")
	if err != nil {
		return fmt.Errorf("drop owned schema: %w", err)
	}
	return nil
}
