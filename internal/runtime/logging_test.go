package runtime

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/telemetry"
)

func TestLoggingComposesWithRuntime(t *testing.T) {
	for _, path := range []string{"/orders/42?token=private", "/missing?token=private"} {
		t.Run(path, func(t *testing.T) {
			rt, err := New(Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(http.MethodGet, "/orders/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			})); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			h := telemetry.Logging(slog.New(slog.NewJSONHandler(&logs, nil)), func(*http.Request) string { return "/orders/*" }, rt)
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Request-ID", strings.Repeat("a", 32))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			id := w.Header().Get("X-Request-ID")
			if len(id) != 32 || id == req.Header.Get("X-Request-ID") {
				t.Fatalf("untrusted request ID %q", id)
			}
			wantStatus := http.StatusServiceUnavailable
			if strings.HasPrefix(path, "/missing") {
				wantStatus = http.StatusNotFound
				var body struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.RequestID != id {
					t.Fatalf("body ID %q != header %q", body.RequestID, id)
				}
			}
			if w.Code != wantStatus {
				t.Fatalf("status %d != %d", w.Code, wantStatus)
			}
			var record struct {
				RequestID string `json:"request_id"`
				Route     string `json:"route"`
				Status    int    `json:"status"`
			}
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.RequestID != id || record.Status != wantStatus || record.Route != "/orders/*" {
				t.Fatalf("inconsistent log: %+v", record)
			}
			if strings.Contains(logs.String(), "private") {
				t.Fatal("query leaked to logs")
			}
		})
	}
}
