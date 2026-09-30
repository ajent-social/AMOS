//go:build integration

package harness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ajent-social/amos/internal/testkit"
)

func TestHTTPBoundaryPersistsThroughRealPostgres(t *testing.T) {
	db, schema := testkit.NewPostgres(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE harness_records (body text NOT NULL)`); err != nil {
		t.Fatalf("create test-only record table: %v", err)
	}

	var actualSchema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&actualSchema); err != nil {
		t.Fatalf("read PostgreSQL schema for HTTP harness: %v", err)
	}
	if actualSchema != schema {
		t.Fatalf("HTTP harness schema = %q, want isolated schema %q", actualSchema, schema)
	}

	server := httptest.NewServer(newBoundaryHandler(db))
	t.Cleanup(server.Close)
	client := server.Client()

	deniedRequest, err := http.NewRequest(http.MethodPost, server.URL+"/_test/harness/records", strings.NewReader(`{"body":"denied"}`))
	if err != nil {
		t.Fatalf("build unauthenticated persistence request: %v", err)
	}
	deniedResponse, err := client.Do(deniedRequest)
	if err != nil {
		t.Fatalf("send unauthenticated persistence request: %v", err)
	}
	assertHTTPResponse(t, deniedResponse, http.StatusUnauthorized, "{\"error\":\"authentication_required\"}\n")

	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM harness_records").Scan(&count); err != nil {
		t.Fatalf("count records after denied HTTP request: %v", err)
	}
	if count != 0 {
		t.Fatalf("unauthenticated request wrote %d records, want zero", count)
	}

	const payload = "synthetic record persisted by the HTTP fixture"
	request, err := http.NewRequest(http.MethodPost, server.URL+"/_test/harness/records", strings.NewReader(`{"body":"`+payload+`"}`))
	if err != nil {
		t.Fatalf("build authenticated persistence request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer fixture-only-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send authenticated persistence request: %v", err)
	}
	assertHTTPResponse(t, response, http.StatusCreated, "{\"persisted\":true}\n")

	var persisted string
	if err := db.QueryRowContext(ctx, "SELECT body FROM harness_records").Scan(&persisted); err != nil {
		t.Fatalf("read record written through HTTP route: %v", err)
	}
	if persisted != payload {
		t.Fatalf("persisted HTTP payload = %q, want %q", persisted, payload)
	}
}
