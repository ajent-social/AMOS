package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReservedAndCanonicalRouteConflicts(t *testing.T) {
	a, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := a.RegisterBusinessRoute("GET", "/SiGnIn/reauth", h); !errors.Is(err, ErrReservedRoute) {
		t.Fatalf("reserved registration: %v", err)
	}
	if err := a.RegisterBusinessRoute("get", "/catalog/", h); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterBusinessRoute("GET", "/catalog", h); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("normalized duplicate: %v", err)
	}
	if err := a.RegisterBusinessRoute("GET", "/reports/*", h); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterBusinessRoute("GET", "/reports/current", h); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("wildcard shadow: %v", err)
	}
}

func TestHealthReadinessAndReservedUnavailable(t *testing.T) {
	a, err := New(Options{ReadinessChecks: []ReadinessCheck{func(context.Context) error { return errors.New("private backend detail") }}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterBusinessRoute("GET", "/catalog", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		want int
		body string
	}{{"/healthz", 200, "ok"}, {"/readyz", 503, "dependency_unavailable"}, {"/signin", 503, "route_unavailable"}, {"/catalog", 201, ""}} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		body := w.Body.String()
		if w.Code != tc.want {
			t.Errorf("%s status=%d want=%d", tc.path, w.Code, tc.want)
		}
		if tc.body != "" && !strings.Contains(body, tc.body) {
			t.Errorf("%s missing %q", tc.path, tc.body)
		}
		if strings.Contains(body, "private backend detail") {
			t.Errorf("dependency detail leaked")
		}
	}
}

func TestServeContextGracefullyDrainsActiveRequest(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	a, err := New(Options{ShutdownTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterBusinessRoute("GET", "/slow", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { close(started); <-release; w.WriteHeader(200) })); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener unavailable for graceful HTTP test: %v", err)
	}
	addr := ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- a.Serve(ctx, ln) }()
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err == nil {
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()
	select {
	case <-time.After(100 * time.Millisecond):
	case <-done:
		t.Fatal("server stopped before active handler drained")
	}
	close(release)
	select {
	case err := <-clientDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop")
	}
}
