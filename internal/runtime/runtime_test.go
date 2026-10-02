package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRejectsAmbiguousIngressPaths(t *testing.T) {
	r, err := New(Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/a//b", "/a/../b", "/a/%2e%2e/b", "/a%2fb", "/a%5cb", "/a%252fb", "/a/%252e%252e/b", "/a%00b", "/a%0ab"} {
		req := httptest.NewRequest("GET", "http://example.test/", nil)
		req.RequestURI = path
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q status=%d want 400", path, w.Code)
		}
	}
	for _, path := range []string{"/a/\xff", "/a/%ff"} {
		req := httptest.NewRequest("GET", "http://example.test/", nil)
		req.RequestURI = path
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid UTF-8 %q status=%d want 400", path, w.Code)
		}
	}
	if _, err := normalizePath("/%61", true); err == nil {
		t.Fatal("registration accepted percent encoding")
	}
}

func TestFiniteFederationCallbackComposition(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	runtime, err := New(Options{Identity: IdentityHandlers{FederationCallback: handler}, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/oauth/callback", http.StatusNoContent},
		{"POST", "/oauth/callback", http.StatusServiceUnavailable},
		{"GET", "/oauth/unconfigured", http.StatusServiceUnavailable},
	} {
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.want {
			t.Fatalf("%s %s: got %d want %d", test.method, test.path, response.Code, test.want)
		}
	}
}
