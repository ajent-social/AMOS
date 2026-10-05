package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLocalHealthUsesDatabaseAndSchemaReadiness(t *testing.T) {
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("open local test listener")
	}
	origin := "http://" + listener.Addr().String()
	cfg := localConfig(t, dsn, origin)
	ctx, cancel := context.WithCancel(context.Background())
	host, err := NewLocal(ctx, cfg)
	if err != nil {
		cancel()
		_ = listener.Close()
		t.Fatal("compose local host with migrated database")
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- host.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case serveErr := <-serveDone:
			if serveErr != nil {
				t.Error("local health host failed while stopping")
			}
		case <-time.After(5 * time.Second):
			_ = listener.Close()
			t.Error("local health host did not stop")
		}
		if closeErr := host.Close(); closeErr != nil {
			t.Error("local health host failed to close")
		}
	})
	client := &http.Client{Timeout: 5 * time.Second}

	status, headers, body := requestHealth(t, client, origin+"/healthz")
	if status != http.StatusOK || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("liveness status/cache = %d/%q, want 200/no-store", status, headers.Get("Cache-Control"))
	}
	var liveness struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &liveness); err != nil || liveness.Status != "ok" {
		t.Fatal("liveness response was not healthy JSON")
	}

	status, headers, body = requestHealth(t, client, origin+"/readyz")
	if status != http.StatusOK || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("readiness status/cache = %d/%q, want 200/no-store", status, headers.Get("Cache-Control"))
	}
	var readiness struct {
		Status       string            `json:"status"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(body, &readiness); err != nil {
		t.Fatal("readiness response was not JSON")
	}
	if readiness.Status != "ready" || readiness.Dependencies["database"] != "available" || readiness.Dependencies["migrations"] != "available" {
		t.Fatalf("unexpected healthy readiness state: status=%q dependencies=%v", readiness.Status, readiness.Dependencies)
	}

	// Keep PostgreSQL reachable while creating a schema mismatch. This exercises
	// the migration/schema gate, not a simulated database outage.
	if err := host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `ALTER TABLE identity_auth_limits DROP CONSTRAINT identity_auth_limits_operation_check`)
		return err
	}); err != nil {
		t.Fatal("create isolated schema readiness failure")
	}
	status, _, body = requestHealth(t, client, origin+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("schema-incompatible readiness status = %d, want 503", status)
	}
	if strings.Contains(string(body), dsn) || strings.Contains(strings.ToLower(string(body)), "constraint") {
		t.Fatal("unavailable readiness exposed database diagnostics")
	}
	var unavailable struct {
		Status       string            `json:"status"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(body, &unavailable); err != nil {
		t.Fatal("unavailable readiness response was not JSON")
	}
	if unavailable.Status != "unavailable" || unavailable.Dependencies["database"] != "available" || unavailable.Dependencies["migrations"] != "unavailable" {
		t.Fatalf("unexpected schema failure state: status=%q dependencies=%v", unavailable.Status, unavailable.Dependencies)
	}
	status, _, _ = requestHealth(t, client, origin+"/healthz")
	if status != http.StatusOK {
		t.Fatalf("liveness depended on schema readiness: status=%d", status)
	}
}

func requestHealth(t *testing.T, client *http.Client, endpoint string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal("construct health request")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("health endpoint request failed")
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			t.Error("close health response")
		}
	}()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal("read health response")
	}
	return res.StatusCode, res.Header.Clone(), body
}
