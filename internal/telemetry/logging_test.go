package telemetry

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appRuntime "github.com/ajent-social/amos/internal/runtime"
)

func TestLogging(t *testing.T) {
	t.Parallel()
	const bearer = "Bearer secret-token-value"
	const cookie = "session=secret-cookie-value"
	cases := []struct {
		name       string
		handler    http.Handler
		wantStatus int
		wantCode   string
	}{
		{name: "success", handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), wantStatus: http.StatusNoContent},
		{name: "error with credential input", handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"auth.denied","message":"` + bearer + `"}`))
		}), wantStatus: http.StatusUnauthorized, wantCode: "auth.denied"},
		{name: "panic boundary", handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(bearer) }), wantStatus: http.StatusInternalServerError, wantCode: "handler.panic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			h := Logging(logger, func(*http.Request) string { return "/orders/{order_id}" }, tc.handler)
			req := httptest.NewRequest(http.MethodPost, "/orders/private-value?token=query-secret", strings.NewReader("body-secret"))
			req.Header.Set("Authorization", bearer)
			req.Header.Set("Cookie", cookie)
			req.Header.Set("X-Request-ID", "external-correlation-value")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tc.wantStatus)
			}
			if id := res.Header().Get("X-Request-ID"); id == "" || id == "external-correlation-value" || !validID.MatchString(id) {
				t.Fatalf("unsafe request ID response header %q", id)
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatalf("decode structured log: %v", err)
			}
			if record["route"] != "/orders/{order_id}" || record["method"] != http.MethodPost || record["status"] != float64(tc.wantStatus) {
				t.Fatalf("missing request fields: %#v", record)
			}
			if record["error_code"] != tc.wantCode {
				t.Fatalf("error_code = %#v, want %#v", record["error_code"], tc.wantCode)
			}
			if _, ok := record["duration_ms"]; !ok {
				t.Fatalf("missing duration: %#v", record)
			}
			for _, secret := range []string{bearer, cookie, "secret-cookie-value", "query-secret", "body-secret", "private-value", "external-correlation-value"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("log contains sensitive input %q", secret)
				}
			}
		})
	}
}

func TestLoggingMalformedCorrelationAndErrorCode(t *testing.T) {
	var logs bytes.Buffer
	h := Logging(slog.New(slog.NewJSONHandler(&logs, nil)), nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "bad\nvalue")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"bad\ncode","private":"do-not-log"}`))
	}))
	req := httptest.NewRequest(http.MethodGet, "/raw-secret-path?password=secret", nil)
	req.Header.Set("X-Request-ID", "bad\nexternal")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Header().Get("X-Request-ID") == "bad\nvalue" || !validID.MatchString(res.Header().Get("X-Request-ID")) {
		t.Fatalf("malformed ID was not replaced: %q", res.Header().Get("X-Request-ID"))
	}
	if !strings.Contains(logs.String(), `"error_code":"http.error"`) {
		t.Fatalf("invalid response code was not sanitized: %s", logs.String())
	}
	for _, forbidden := range []string{"raw-secret-path", "password", "secret", "private", "bad\\n"} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("log contains forbidden value %q", forbidden)
		}
	}
}

func TestLoggingAcceptsBoundedCorrelationID(t *testing.T) {
	t.Parallel()
	const correlationID = "0123456789abcdef0123456789abcdef"
	var logs bytes.Buffer
	h := Logging(slog.New(slog.NewJSONHandler(&logs, nil)), nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", correlationID)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if got := res.Header().Get("X-Request-ID"); got != correlationID {
		t.Fatalf("response ID = %q, want accepted correlation ID", got)
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] != correlationID {
		t.Fatalf("logged ID = %#v, want accepted correlation ID", record["request_id"])
	}
}

func TestLoggingUsesRuntimeRequestID(t *testing.T) {
	const runtimeID = "0123456789abcdef0123456789abcdef"
	var logs bytes.Buffer
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", runtimeID)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"route.not_found"}`))
	})
	h := Logging(slog.New(slog.NewJSONHandler(&logs, nil)), nil, inner)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Header().Get("X-Request-ID") != runtimeID {
		t.Fatalf("response ID = %q, want runtime ID", res.Header().Get("X-Request-ID"))
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] != runtimeID {
		t.Fatalf("logged ID = %#v, want runtime ID", record["request_id"])
	}
}

func TestLoggingComposesWithRuntime(t *testing.T) {
	var logs bytes.Buffer
	runtime, err := appRuntime.New(appRuntime.Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register(http.MethodGet, "/orders/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})); err != nil {
		t.Fatal(err)
	}
	h := Logging(slog.New(slog.NewJSONHandler(&logs, nil)), func(*http.Request) string { return "/orders/*" }, runtime)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/orders/42?token=private", nil))
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
	id := res.Header().Get("X-Request-ID")
	if !validID.MatchString(id) {
		t.Fatalf("runtime request ID is invalid: %q", id)
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] != id || record["route"] != "/orders/*" || record["status"] != float64(http.StatusServiceUnavailable) {
		t.Fatalf("log does not match runtime response: %#v", record)
	}
}

func TestLoggingErrorBodyBufferBound(t *testing.T) {
	var logs bytes.Buffer
	h := Logging(slog.New(slog.NewJSONHandler(&logs, nil)), nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1<<20))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got := len(logs.Bytes()); got > 1024 {
		t.Fatalf("logged error body data; record size %d", got)
	}
}
