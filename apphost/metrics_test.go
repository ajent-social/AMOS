package apphost

import (
	"context"
	"github.com/ajent-social/amos/observability"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMetricsLocalConfigRequiresFixedWorkerBeforeDependencies(t *testing.T) {
	c, _ := observability.New(observability.Config{})
	if _, err := NewLocal(context.Background(), LocalConfig{Metrics: c}); err != ErrLocalConfiguration {
		t.Fatal("missing fixed worker name not rejected", err)
	}
}
func TestMetricsLocalHostCycle(t *testing.T) {
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	cfg := localConfig(t, dsn, "http://"+listener.Addr().String())
	c, _ := observability.New(observability.Config{RouteTemplates: []string{"/healthz"}, WorkerNames: []string{"local-email"}})
	cfg.Metrics = c
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewLocal(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	h.app.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if c.Snapshot().HTTP[0].Status[1] != 1 {
		t.Fatal("App collector not wired")
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, listener) }()
	deadline := time.Now().Add(4 * time.Second)
	for c.Snapshot().Workers[0].Queue.State != "observed" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	s := c.Snapshot()
	if s.Workers[0].Queue.State != "observed" || s.Database.State != "observed" || s.Workers[0].Outcomes[0] == 0 {
		t.Fatalf("cycle not wired %+v", s)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if err := h.pool.Close(); err != nil {
		t.Fatal(err)
	}
	h.observeMetrics(context.Background())
	if c.Snapshot().Workers[0].Queue.State != "stale" {
		t.Fatal("collection failure hidden")
	}
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	h.observeMetrics(context.Background())
	if c.Snapshot().Database.State != "stale" {
		t.Fatal("closed database stats healthy")
	}
	r := httptest.NewRequest(http.MethodGet, "/operator", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("private output unavailable")
	}
}
