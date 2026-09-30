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
	for _, path := range []string{"/a//b", "/a/../b", "/a/%2e%2e/b", "/a%2fb", "/a%5cb"} {
		req := httptest.NewRequest("GET", "http://example.test/", nil)
		req.RequestURI = path
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q status=%d want 400", path, w.Code)
		}
	}
}
