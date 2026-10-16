package telemetryconsumer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/app"
)

// This consumer deliberately lives outside the AMOS module and imports only
// its public App API. The backend is a real second HTTP server. Its correlation
// field is diagnostic data, never identity or authorization evidence.
type logSink struct {
	mu   sync.Mutex
	data []byte
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, p...)
	return len(p), nil
}
func (s *logSink) snapshot() []byte { s.mu.Lock(); defer s.mu.Unlock(); return bytes.Clone(s.data) }
func records(t *testing.T, sink *logSink, want int) []map[string]any {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		body := sink.snapshot()
		lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
		if len(body) > 0 && len(lines) >= want {
			if len(lines) != want {
				t.Fatalf("log record count=%d want=%d", len(lines), want)
			}
			result := make([]map[string]any, len(lines))
			for i, line := range lines {
				if err := json.Unmarshal(line, &result[i]); err != nil {
					t.Fatal("decode structured record", err)
				}
			}
			return result
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d log records", want)
		}
	}
}

func TestPublicAppCorrelatesPrivateHTTPServiceWithoutInputLeakage(t *testing.T) {
	validID := regexp.MustCompile(`^[a-f0-9]{32}$`)
	const bearer = "Bearer bearer-canary"
	const cookie = "session=cookie-canary"
	const providerKey = "provider-key-canary"
	const bodyCanary = "body-canary"
	const queryCanary = "query-canary"
	const diagnostic = "backend-diagnostic-canary"
	var appLogs, backendLogs logSink
	backendLogger := slog.New(slog.NewJSONHandler(&backendLogs, nil))
	problems := make(chan string, 32)
	observed := make(chan string, 16)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.Header.Values("X-Request-ID")
		if len(ids) != 1 || !validID.MatchString(ids[0]) {
			problems <- "backend received invalid or duplicate correlation"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, name := range []string{"Authorization", "Cookie", "X-Provider-Key", "X-Forwarded-User"} {
			if r.Header.Get(name) != "" {
				problems <- "backend received caller credential or identity header"
			}
		}
		if r.URL.RawQuery != "" || r.URL.Path != "/internal/work" {
			problems <- "backend received caller request target"
		}
		payload, err := io.ReadAll(io.LimitReader(r.Body, 128))
		if err != nil || string(payload) != "safe-work" {
			problems <- "backend received unexpected body"
		}
		status := http.StatusNoContent
		if r.Method == http.MethodPost {
			status = http.StatusServiceUnavailable
		}
		backendLogger.Info("business request", "request_id", ids[0], "route", "/internal/work", "method", r.Method, "status", status)
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = io.WriteString(w, diagnostic)
		}
	}))
	defer backend.Close()
	application, err := app.New(app.Options{Logger: slog.New(slog.NewJSONHandler(&appLogs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	backendClient := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	err = application.RegisterBusinessRoute(http.MethodPost, "/business/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the value installed by AMOS before route dispatch, never the
		// untrusted inbound request header or an internal-package context getter.
		requestID := w.Header().Get("X-Request-ID")
		observed <- requestID
		if !validID.MatchString(requestID) {
			problems <- "business handler did not receive generated correlation"
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()
		method := http.MethodGet
		if r.URL.Path == "/business/failure" {
			method = http.MethodPost
		}
		request, err := http.NewRequestWithContext(ctx, method, backend.URL+"/internal/work", strings.NewReader("safe-work"))
		if err != nil {
			problems <- "construct private service request"
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		request.Header.Set("X-Request-ID", requestID)
		response, err := backendClient.Do(request)
		if err != nil {
			problems <- "private service request failed"
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			problems <- "private service response failed"
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if response.StatusCode != http.StatusNoContent {
			http.Error(w, "operation unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	frontend := httptest.NewServer(application.Handler())
	defer frontend.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	cases := []struct {
		name, path string
		ids        []string
		status     int
	}{
		{"success", "/business/success", []string{"0123456789abcdef0123456789abcdef"}, http.StatusNoContent},
		{"failure", "/business/failure", []string{"fedcba9876543210fedcba9876543210"}, http.StatusServiceUnavailable},
		{"malformed correlation", "/business/success", []string{"<caller-correlation-canary>"}, http.StatusNoContent},
		{"duplicate correlation", "/business/failure", []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, http.StatusServiceUnavailable},
		{"missing correlation", "/business/success", nil, http.StatusNoContent},
	}
	seen := make(map[string]bool)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, frontend.URL+tc.path+"?token="+queryCanary, strings.NewReader(bodyCanary))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range tc.ids {
				request.Header.Add("X-Request-ID", id)
			}
			request.Header.Set("Authorization", bearer)
			request.Header.Set("Cookie", cookie)
			request.Header.Set("X-Provider-Key", providerKey)
			request.Header.Set("X-Forwarded-User", "identity-canary")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("read response=%v close=%v", readErr, closeErr)
			}
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", response.StatusCode, tc.status)
			}
			id := response.Header.Get("X-Request-ID")
			if !validID.MatchString(id) || seen[id] {
				t.Fatal("response correlation is invalid or reused")
			}
			seen[id] = true
			for _, input := range tc.ids {
				if id == input {
					t.Fatal("caller correlation reused as trusted ID")
				}
			}
			select {
			case handlerID := <-observed:
				if handlerID != id {
					t.Fatal("handler and response correlation differ")
				}
			case <-time.After(time.Second):
				t.Fatal("business handler observation missing")
			}
			appRecords := records(t, &appLogs, i+1)
			backendRecords := records(t, &backendLogs, i+1)
			a, b := appRecords[i], backendRecords[i]
			if a["request_id"] != id || b["request_id"] != id {
				t.Fatal("App and separate service logs are not correlated")
			}
			if a["route"] != "/business/*" || a["method"] != http.MethodPost || a["status"] != float64(tc.status) || b["route"] != "/internal/work" || b["status"] != float64(tc.status) {
				t.Fatal("correlated records lost safe route, method or status")
			}
			if _, ok := a["duration_ms"]; !ok {
				t.Fatal("App duration missing")
			}
			if tc.status >= 400 && a["error_code"] != "http.error" {
				t.Fatal("failed request did not retain safe error code")
			}
			for _, sink := range []*logSink{&appLogs, &backendLogs} {
				encoded := string(sink.snapshot())
				forbidden := append([]string{bearer, cookie, "cookie-canary", providerKey, bodyCanary, queryCanary, diagnostic, "identity-canary", tc.path}, tc.ids...)
				for _, canary := range forbidden {
					if strings.Contains(encoded, canary) {
						t.Fatalf("encoded log contains synthetic private input %q", canary)
					}
				}
			}
			select {
			case issue := <-problems:
				t.Fatal(issue)
			default:
			}
		})
	}
	select {
	case issue := <-problems:
		t.Fatal(issue)
	default:
	}
	if len(seen) != len(cases) {
		t.Fatalf("completed %d unique requests, want %d", len(seen), len(cases))
	}
}
