//go:build integration

package testkit

import (
	"context"
	"testing"
)

func TestNewPostgresCreatesIsolatedSchema(t *testing.T) {
	db, schema := NewPostgres(t)

	var currentSchema string
	if err := db.QueryRowContext(context.Background(), "SELECT current_schema()").Scan(&currentSchema); err != nil {
		t.Fatalf("read current PostgreSQL schema: %v", err)
	}
	if currentSchema != schema {
		t.Fatalf("current schema = %q, want isolated schema %q", currentSchema, schema)
	}
	if _, err := db.ExecContext(context.Background(), "CREATE TABLE isolated_probe (id integer PRIMARY KEY)"); err != nil {
		t.Fatalf("create table in isolated schema: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), "INSERT INTO isolated_probe (id) VALUES (1)"); err != nil {
		t.Fatalf("write to isolated schema: %v", err)
	}
}
