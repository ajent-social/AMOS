package app

import (
	"context"
	"encoding/json"
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
	for _, path := range []string{"/signup", "/auth/callback", "/verify-email", "/forgot-password", "/reset-password", "/oauth/return", "/.well-known/openid-configuration"} {
		if err := a.RegisterBusinessRoute("GET", path, h); !errors.Is(err, ErrReservedRoute) {
			t.Errorf("%s registration error=%v", path, err)
		}
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
	}{{"/healthz", 200, "ok"}, {"/readyz", 503, "dependency.unavailable"}, {"/signin", 503, "route.unavailable"}, {"/catalog", 201, ""}} {
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

func TestErrorsUseServerGeneratedRequestID(t *testing.T) {
	a, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, path := range []string{"/unknown", "/signin"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Request-ID", "attacker-chosen")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Header().Get("X-Request-ID") == "attacker-chosen" || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("untrusted or missing response request ID: %q", w.Header().Get("X-Request-ID"))
		}
		var body struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatalf("body/header request IDs differ: %#v %q", body, w.Header().Get("X-Request-ID"))
		}
		if ids[body.RequestID] {
			t.Fatalf("request ID reused: %q", body.RequestID)
		}
		ids[body.RequestID] = true
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
		t.Fatalf("required real HTTP listener unavailable: %v", err)
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

func TestBusinessWildcardAndDistinctApexCanBothServe(t *testing.T) {
	a, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	apex := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
	child := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	if err := a.RegisterBusinessRoute("GET", "/todos", apex); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterBusinessRoute("GET", "/todos/*", child); err != nil {
		t.Fatal("nonoverlapping apex and child routes rejected", err)
	}
	if err := a.RegisterBusinessRoute("GET", "/todos/*", child); !errors.Is(err, ErrRouteConflict) {
		t.Fatal("duplicate wildcard accepted", err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/todos", 202}, {"/todos/one/edit", 204}} {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("path=%s status=%d", tc.path, w.Code)
		}
	}
}
