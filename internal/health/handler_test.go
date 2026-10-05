package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type testPinger struct{ err error }

func (p testPinger) PingContext(context.Context) error { return p.err }

func TestHTTP(t *testing.T) {
	t.Parallel()
	privateDetail := "postgres://user:secret@private.internal/db SELECT private_data"
	tests := []struct {
		name       string
		path       string
		method     string
		requestID  string
		dbErr      error
		migration  Check
		optional   []OptionalCheck
		wantStatus int
		wantBody   []string
		wantAbsent []string
	}{
		{name: "live independent of database", path: LivenessPath, method: http.MethodGet, dbErr: errors.New(privateDetail), migration: func(context.Context) error { return nil }, wantStatus: http.StatusOK, wantBody: []string{`"status":"ok"`, `"checked_at":`}, wantAbsent: []string{"secret", "private.internal", "SELECT"}},
		{name: "ready", path: ReadinessPath, method: http.MethodGet, requestID: "server-generated-id", migration: func(context.Context) error { return nil }, wantStatus: http.StatusOK, wantBody: []string{`"status":"ready"`, `"checked_at":`, `"request_id":"server-generated-id"`, `"database":"available"`, `"migrations":"available"`}},
		{name: "database unavailable", path: ReadinessPath, method: http.MethodGet, requestID: "server-generated-id", dbErr: errors.New(privateDetail), migration: func(context.Context) error { return nil }, wantStatus: http.StatusServiceUnavailable, wantBody: []string{`"status":"unavailable"`, `"code":"dependency.unavailable"`, `"request_id":"server-generated-id"`, `"database":"unavailable"`}, wantAbsent: []string{"secret", "private.internal", "SELECT"}},
		{name: "migration incompatible", path: ReadinessPath, method: http.MethodGet, migration: func(context.Context) error { return errors.New(privateDetail) }, wantStatus: http.StatusServiceUnavailable, wantAbsent: []string{"secret", "private.internal", "SELECT"}},
		{name: "optional provider degraded", path: ReadinessPath, method: http.MethodGet, migration: func(context.Context) error { return nil }, optional: []OptionalCheck{{Name: "mail", Check: func(context.Context) error { return errors.New(privateDetail) }}}, wantStatus: http.StatusOK, wantBody: []string{`"status":"ready"`, `"degraded":true`, `"mail":"unavailable"`}, wantAbsent: []string{"secret", "private.internal", "SELECT"}},
		{name: "head omits body", path: LivenessPath, method: http.MethodHead, migration: func(context.Context) error { return nil }, wantStatus: http.StatusOK},
		{name: "method rejected", path: LivenessPath, method: http.MethodPost, requestID: "server-generated-id", migration: func(context.Context) error { return nil }, wantStatus: http.StatusMethodNotAllowed, wantBody: []string{`"code":"method.not_allowed"`, `"request_id":"server-generated-id"`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, err := NewHandler(Options{Database: testPinger{err: tc.dbErr}, MigrationCheck: tc.migration, OptionalChecks: tc.optional, ReadinessTimeout: time.Second})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			if tc.requestID != "" {
				recorder.Header().Set("X-Request-ID", tc.requestID)
			}
			h.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if tc.method == http.MethodHead && recorder.Body.Len() != 0 {
				t.Errorf("HEAD response body = %q, want empty", recorder.Body.String())
			}
			for _, fragment := range tc.wantBody {
				if !strings.Contains(recorder.Body.String(), fragment) {
					t.Errorf("body %q does not contain %q", recorder.Body.String(), fragment)
				}
			}
			for _, fragment := range tc.wantAbsent {
				if strings.Contains(recorder.Body.String(), fragment) {
					t.Errorf("body disclosed forbidden detail %q", fragment)
				}
			}
		})
	}
}

func TestReadinessTimeoutIsUnavailable(t *testing.T) {
	t.Parallel()
	h, err := NewHandler(Options{
		Database: testPinger{},
		MigrationCheck: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		ReadinessTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP status = %d, want 503", recorder.Code)
	}
}

func TestOptionalReadinessTimeoutDegrades(t *testing.T) {
	t.Parallel()
	checks := []struct {
		name  string
		check Check
	}{
		{
			name: "returns context error",
			check: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
		{
			name: "returns nil after context expires",
			check: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
		},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, err := NewHandler(Options{
				Database:         testPinger{},
				MigrationCheck:   func(context.Context) error { return nil },
				OptionalChecks:   []OptionalCheck{{Name: "mail", Check: tc.check}},
				ReadinessTimeout: 10 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200", recorder.Code)
			}
			for _, fragment := range []string{`"status":"ready"`, `"degraded":true`, `"mail":"unavailable"`} {
				if !strings.Contains(recorder.Body.String(), fragment) {
					t.Errorf("body %q does not contain %q", recorder.Body.String(), fragment)
				}
			}
		})
	}
}

func TestParentRequestCancellationDoesNotBecomeOptionalDegradation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	h, err := NewHandler(Options{
		Database:       testPinger{},
		MigrationCheck: func(context.Context) error { return nil },
		OptionalChecks: []OptionalCheck{{
			Name: "mail",
			Check: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			},
		}},
		ReadinessTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, ReadinessPath, nil).WithContext(requestContext))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("optional check did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop readiness")
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" {
		t.Fatalf("canceled request wrote a response body/header: body=%q content-type=%q", recorder.Body.String(), recorder.Header().Get("Content-Type"))
	}
}

func TestNewHandlerRejectsMissingRequiredChecks(t *testing.T) {
	t.Parallel()
	_, err := NewHandler(Options{ReadinessTimeout: time.Second})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewHandler() error = %v, want ErrInvalidOptions", err)
	}
}

func TestHTTPPostgresDisconnectAndRestore(t *testing.T) {
	if os.Getenv("AMOS_HEALTH_POSTGRES_REQUIRED") != "1" {
		t.Skip("NOT_RUN: set AMOS_HEALTH_POSTGRES_REQUIRED=1 with AMOS_TEST_DATABASE_URL to run the real PostgreSQL health check")
	}

	db, _ := testkit.NewPostgres(t)
	newHandler := func(database interface{ PingContext(context.Context) error }) *Handler {
		h, err := NewHandler(Options{
			Database: database,
			MigrationCheck: func(ctx context.Context) error {
				return database.PingContext(ctx)
			},
			ReadinessTimeout: time.Second,
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		return h
	}
	serve := func(h http.Handler, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}
	ready := newHandler(db)
	if got := serve(ready, ReadinessPath).Code; got != http.StatusOK {
		t.Fatalf("connected database readiness status = %d, want 200", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close isolated database fixture: %v", err)
	}
	if got := serve(ready, ReadinessPath).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("disconnected database readiness status = %d, want 503", got)
	}
	if got := serve(ready, LivenessPath).Code; got != http.StatusOK {
		t.Fatalf("liveness with disconnected database status = %d, want 200", got)
	}

	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse validated test database URL: %v", err)
	}
	restoredDB := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if err := restoredDB.Close(); err != nil {
			t.Errorf("close restored test database pool: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := restoredDB.PingContext(ctx); err != nil {
		t.Fatalf("restore database connectivity: %v", err)
	}
	if got := serve(newHandler(restoredDB), ReadinessPath).Code; got != http.StatusOK {
		t.Fatalf("restored database readiness status = %d, want 200", got)
	}
}
