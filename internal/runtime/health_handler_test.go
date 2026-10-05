package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDelegatedHealthPreservesReservedSurface(t *testing.T) {
	var paths []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("X-Request-ID") == "" {
			t.Error("missing server request ID")
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})
	rt, err := New(Options{HealthHandler: h, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			rt.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusAccepted {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
	for _, path := range []string{"/healthz/child", "/readyz/child", "/business"} {
		rt.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/readyz", nil))
	if w.Code != http.StatusMethodNotAllowed || len(paths) != 4 {
		t.Fatalf("delegated invalid surface: status=%d paths=%v", w.Code, paths)
	}
	if err := rt.Register(http.MethodGet, "/readyz", h); err != ErrReservedRoute {
		t.Fatalf("reserved route error: %v", err)
	}
}

func TestHealthHandlerRejectsCompetingReadinessAuthority(t *testing.T) {
	_, err := New(Options{HealthHandler: http.NotFoundHandler(), ReadinessChecks: []func(context.Context) error{func(context.Context) error { return nil }}, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != ErrInvalidOptions {
		t.Fatalf("error=%v", err)
	}
}
