package app_test

import (
	"fmt"
	"github.com/ajent-social/amos/app"
	"github.com/ajent-social/amos/observability"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsActualApp(t *testing.T) {
	c, err := observability.New(observability.Config{RouteTemplates: []string{"/ok", "/error", "/panic"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(app.Options{Metrics: c, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	for route, h := range map[string]http.HandlerFunc{"/ok": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }, "/error": func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"code":"dependency.unavailable","secret":"CANARY"}`))
	}, "/panic": func(w http.ResponseWriter, r *http.Request) { panic("PANIC-CANARY") }} {
		if err := a.RegisterBusinessRoute("GET", route, h); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"/ok", "/error", "/panic", "/missing-CANARY?email=secret@example.test"} {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", p, nil))
	}
	s := c.Snapshot()
	if s.HTTP[0].Status[1] != 1 || s.HTTP[1].Status[4] != 1 || s.HTTP[1].Errors[2] != 1 || s.HTTP[2].Errors[3] != 1 || s.HTTP[4].Status[3] != 1 {
		t.Fatalf("actual observations %+v", s.HTTP)
	}
	for i := 0; i < 2000; i++ {
		a.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", fmt.Sprintf("/missing-CANARY/%d?private=secret@example.test", i), nil))
	}
	if got := c.Snapshot(); len(got.HTTP) != 5 || got.HTTP[4].DurationCount != 2001 {
		t.Fatal("arbitrary request paths expanded or misclassified series")
	}
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "CANARY") || strings.Contains(w.Body.String(), "example.test") {
		t.Fatal("private material leaked")
	}
	// Metrics never installs an operator route on the public App.
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 404 {
		t.Fatal("metrics publicly exposed")
	}
}
