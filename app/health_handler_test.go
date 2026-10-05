package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerForwardingAndAuthority(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	a, err := New(Options{HealthHandler: h})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("handler not forwarded: %d", w.Code)
	}
	_, err = New(Options{HealthHandler: h, ReadinessChecks: []ReadinessCheck{func(context.Context) error { return nil }}})
	if err != ErrInvalidOptions {
		t.Fatalf("competing authority: %v", err)
	}
	a, err = New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("default readiness changed: %d", w.Code)
	}
}
