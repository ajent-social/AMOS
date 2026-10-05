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

func TestRuntimeAutomaticallyLogsSanitizedRouteTemplate(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	rt, err := New(Options{Logger: logger, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(http.MethodGet, "/orders/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := telemetry.RequestID(r.Context()); got == "" {
			t.Error("runtime handler did not receive trusted request ID")
		}
		http.Error(w, "sensitive response body", http.StatusServiceUnavailable)
	})); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/orders/person@example.test?access_token=example", nil)
	request.Header.Set("Authorization", "Bearer header-secret")
	request.Header.Set("Cookie", "session=cookie-secret")
	request.Header.Set("X-Request-ID", "caller-controlled-id")
	response := httptest.NewRecorder()
	rt.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	requestID := response.Header().Get("X-Request-ID")
	if len(requestID) != 32 || requestID == "caller-controlled-id" {
		t.Fatalf("untrusted request ID was not replaced: %q", requestID)
	}
	record := decodeLog(t, output.Bytes())
	if record["request_id"] != requestID || record["route"] != "/orders/*" || record["status"] != float64(http.StatusServiceUnavailable) || record["error_code"] != "http.error" {
		t.Fatalf("unexpected request log: %#v", record)
	}
	for _, sensitiveInput := range []string{"person@example.test", "example", "header-secret", "cookie-secret", "sensitive response body", "caller-controlled-id"} {
		if strings.Contains(output.String(), sensitiveInput) {
			t.Fatalf("request log contains sensitive value %q", sensitiveInput)
		}
	}
}

func TestRuntimeLogsUnmatchedAndReservedDescendantWithoutRawPath(t *testing.T) {
	for _, path := range []string{"/missing/person@example.test?token=private", "/signup/person@example.test?token=private"} {
		t.Run(path, func(t *testing.T) {
			var output bytes.Buffer
			rt, err := New(Options{Logger: slog.New(slog.NewJSONHandler(&output, nil)), ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			rt.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound && response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want unmatched or reserved-unavailable", response.Code)
			}
			var body struct {
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			record := decodeLog(t, output.Bytes())
			if record["route"] != "unmatched" || record["request_id"] != body.RequestID || response.Header().Get("X-Request-ID") != body.RequestID {
				t.Fatalf("request, response, and log IDs or route differ: body=%+v log=%#v", body, record)
			}
			if strings.Contains(output.String(), "person@example.test") || strings.Contains(output.String(), "private") {
				t.Fatal("unknown or reserved descendant path leaked into request log")
			}
		})
	}
}

func TestRuntimeLogsFixedIdentityTemplateAndPanicsWithoutPanicValue(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		var output bytes.Buffer
		rt, err := New(Options{Logger: slog.New(slog.NewJSONHandler(&output, nil)), ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		rt.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/signin", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 for unavailable reserved identity route", response.Code)
		}
		record := decodeLog(t, output.Bytes())
		if record["route"] != "/signin" || record["status"] != float64(http.StatusServiceUnavailable) || record["error_code"] != "route.unavailable" {
			t.Fatalf("identity route was not logged with its fixed template: %#v", record)
		}
	})

	t.Run("panic", func(t *testing.T) {
		var output bytes.Buffer
		rt, err := New(Options{Logger: slog.New(slog.NewJSONHandler(&output, nil)), ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Register(http.MethodGet, "/panic", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("panic-secret")
		})); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		rt.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", response.Code)
		}
		record := decodeLog(t, output.Bytes())
		if record["route"] != "/panic" || record["status"] != float64(http.StatusInternalServerError) || record["error_code"] != "handler.panic" {
			t.Fatalf("panic request was not logged safely: %#v", record)
		}
		if strings.Contains(output.String(), "panic-secret") {
			t.Fatal("panic value leaked into request log")
		}
	})
}

func TestRuntimeDefaultLoggerIsUsedAndRestored(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	rt, err := New(Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	rt.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.Code)
	}
	record := decodeLog(t, output.Bytes())
	if record["route"] != "/healthz" || record["status"] != float64(http.StatusOK) || record["request_id"] != response.Header().Get("X-Request-ID") {
		t.Fatalf("default logger did not record the runtime request: %#v", record)
	}
}

func decodeLog(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("decode structured request log: %v", err)
	}
	return record
}
