package businessproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestFiltering(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"accepted\":true}")
	}))
	t.Cleanup(upstream.Close)

	builder, err := NewRequestBuilder(Config{
		Origin: upstream.URL,
		Routes: []Route{{
			PublicPrefix:        "/business",
			UpstreamPrefix:      "/v1/customers",
			Methods:             []string{http.MethodGet, http.MethodPost},
			QueryKeys:           []QueryKey{{Name: "filter"}},
			RequestContentTypes: []string{"application/json"},
		}},
	})
	if err != nil {
		t.Fatalf("NewRequestBuilder(): %v", err)
	}

	upstreamClient, err := NewHTTPClient(upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, in *http.Request) {
		out, cancel, err := builder.Build(in)
		if err != nil {
			writeBuildError(w, err)
			return
		}
		defer cancel()

		response, err := upstreamClient.Do(out)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close upstream response body: %v", err)
			}
		}()
		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			t.Errorf("copy upstream response: %v", err)
		}
	}))
	t.Cleanup(front.Close)

	request, err := http.NewRequest(http.MethodPost, front.URL+"/business/42?filter=active", strings.NewReader(`{"name":"sample"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer attacker")
	request.Header.Set("Cookie", "session=attacker")
	request.Header.Set("Forwarded", "for=attacker;host=attacker.invalid")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Forwarded-Host", "attacker.invalid")
	request.Header.Set("X-Real-IP", "203.0.113.9")
	request.Header.Set("X-AMOS-Identity", "unsigned")
	request.Header.Set("X-Tenant-ID", "forged-tenant")
	request.Header.Set("X-Workspace-ID", "forged-workspace")
	request.Header.Set("X-Request-ID", "forged-request")

	response, err := front.Client().Do(request)
	if err != nil {
		t.Fatalf("front proxy request: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read front response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != `{"accepted":true}` {
		t.Fatalf("front response = %d %q, want 200 accepted body", response.StatusCode, body)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestRequestFilteringRewritesOnlyAllowlistedTargetAndHeaders(t *testing.T) {
	var observed *http.Request
	var observedBody []byte
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Clone(r.Context())
		observed.Header = r.Header.Clone()
		observedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	builder := mustBuilder(t, upstream.URL)
	upstreamClient, err := NewHTTPClient(upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	input := httptest.NewRequest(http.MethodPost, "/business/people/42?filter=active", strings.NewReader(`{"name":"sample"}`))
	input.Header.Set("Content-Type", "application/json; charset=utf-8")
	input.Header.Set("Accept", "application/json")
	for name, value := range map[string]string{
		"Authorization":    "Bearer attacker",
		"Cookie":           "session=attacker",
		"Forwarded":        "for=attacker",
		"X-Forwarded-For":  "203.0.113.9",
		"X-Forwarded-Host": "attacker.invalid",
		"X-Real-IP":        "203.0.113.9",
		"X-AMOS-Identity":  "unsigned",
		"X-Tenant-ID":      "forged-tenant",
		"X-Workspace-ID":   "forged-workspace",
		"X-Request-ID":     "forged-request",
		"X-Internal-Trace": "private",
	} {
		input.Header.Set(name, value)
	}

	out, cancel, err := builder.Build(input)
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	defer cancel()
	if out.URL.Scheme != "https" || out.URL.Host != mustURL(t, upstream.URL).Host {
		t.Fatalf("outbound origin = %s, want configured origin %s", out.URL, upstream.URL)
	}
	if out.URL.Path != "/v1/customers/people/42" || out.URL.RawQuery != "filter=active" {
		t.Fatalf("outbound target = %s, want /v1/customers/people/42?filter=active", out.URL)
	}
	if out.Host != mustURL(t, upstream.URL).Host {
		t.Fatalf("outbound Host = %q, want configured upstream host %q", out.Host, mustURL(t, upstream.URL).Host)
	}
	if out.Header.Get("Accept") != "application/json" || out.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("allowed headers not retained: %#v", out.Header)
	}
	for _, name := range []string{"Authorization", "Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP", "X-AMOS-Identity", "X-Tenant-ID", "X-Workspace-ID", "X-Request-ID", "X-Internal-Trace"} {
		if value := out.Header.Get(name); value != "" {
			t.Errorf("untrusted header %s forwarded with value %q", name, value)
		}
	}

	response, err := upstreamClient.Do(out)
	if err != nil {
		t.Fatalf("upstream Do(): %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("upstream status = %d, want 204", response.StatusCode)
	}
	if observed == nil {
		t.Fatal("upstream did not receive the request")
	}
	if observed.Method != http.MethodPost || observed.URL.Path != "/v1/customers/people/42" || observed.URL.RawQuery != "filter=active" {
		t.Fatalf("upstream saw %s %s?%s", observed.Method, observed.URL.Path, observed.URL.RawQuery)
	}
	if string(observedBody) != `{"name":"sample"}` {
		t.Fatalf("upstream body = %q", observedBody)
	}
	for _, name := range []string{"Authorization", "Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP", "X-AMOS-Identity", "X-Tenant-ID", "X-Workspace-ID", "X-Request-ID", "X-Internal-Trace"} {
		if value := observed.Header.Get(name); value != "" {
			t.Errorf("upstream received untrusted header %s=%q", name, value)
		}
	}
}

func TestRequestFilteringRejectsBeforeUpstreamContact(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	builder := mustBuilder(t, upstream.URL)
	upstreamClient, err := NewHTTPClient(upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, in *http.Request) {
		out, cancel, err := builder.Build(in)
		if err != nil {
			writeBuildError(w, err)
			return
		}
		defer cancel()
		response, err := upstreamClient.Do(out)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close upstream response body: %v", err)
			}
		}()
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(front.Close)

	tests := []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		wantStatus  int
	}{
		{
			name:       "double encoded traversal to internal admin path with forged identity",
			method:     http.MethodPost,
			target:     "/business/%252E%252E/admin",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "double encoded traversal into routable path",
			method:     http.MethodGet,
			target:     "/business/%252E%252E/people",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "disallowed method",
			method:     http.MethodDelete,
			target:     "/business/people",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "reserved billing route",
			method:     http.MethodGet,
			target:     "/billing/subscriptions",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "nested internal admin route",
			method:     http.MethodGet,
			target:     "/business/admin",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown query key",
			method:     http.MethodGet,
			target:     "/business/people?workspace=forged",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "duplicate query key",
			method:     http.MethodGet,
			target:     "/business/people?filter=active&filter=disabled",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "encoded separator",
			method:     http.MethodGet,
			target:     "/business/people%2Fadmin",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "encoded dot traversal",
			method:     http.MethodGet,
			target:     "/business/%2e%2e/admin",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:        "oversized request body",
			method:      http.MethodPost,
			target:      "/business/people",
			body:        strings.Repeat("x", maxRequestBodyBytes+1),
			contentType: "application/json",
			wantStatus:  http.StatusRequestEntityTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, front.URL+test.target, strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			request.Header.Set("X-AMOS-Identity", "forged")
			request.Header.Set("X-Tenant-ID", "forged")
			response, err := front.Client().Do(request)
			if err != nil {
				t.Fatalf("front request: %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("front status = %d, want %d", response.StatusCode, test.wantStatus)
			}
		})
	}

	// net/http removes connection-nominated headers while parsing a real
	// server request. Exercise the raw filter boundary directly as well.
	for _, name := range []string{"connection nominated identity header", "hop by hop upgrade", "absolute request target"} {
		t.Run(name, func(t *testing.T) {
			request := requestWithPath(http.MethodGet, "/business/people")
			switch name {
			case "connection nominated identity header":
				request.Header.Set("Connection", "X-Workspace-ID")
				request.Header.Set("X-Workspace-ID", "forged")
			case "hop by hop upgrade":
				request.Header.Set("Upgrade", "websocket")
			case "absolute request target":
				request.URL.Scheme = "https"
				request.URL.Host = "attacker.invalid"
			}
			_, cancel, err := builder.Build(request)
			if cancel != nil {
				cancel()
			}
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Build() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("invalid requests contacted upstream %d times", got)
	}
}

func TestProxyHTTPClientRejectsRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	redirectTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(redirectTarget.Close)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", redirectTarget.URL)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	client, err := NewHTTPClient(origin.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrUpstreamRedirect) {
		t.Fatalf("Do() error = %v, want ErrUpstreamRedirect", err)
	}
	if got := redirectedRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want 0", got)
	}
	if _, err := NewHTTPClient(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewHTTPClient(nil) error = %v, want ErrInvalidRequest", err)
	}
}

func TestNewRequestBuilderRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	base := Config{
		Origin: "https://business.invalid",
		Routes: []Route{{
			PublicPrefix:   "/business",
			UpstreamPrefix: "/v1",
			Methods:        []string{http.MethodGet},
		}},
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"non-TLS origin", func(c *Config) { c.Origin = "http://business.invalid" }},
		{"origin path override", func(c *Config) { c.Origin = "https://business.invalid/other" }},
		{"empty origin query marker", func(c *Config) { c.Origin = "https://business.invalid?" }},
		{"no routes", func(c *Config) { c.Routes = nil }},
		{"reserved public route", func(c *Config) { c.Routes[0].PublicPrefix = "/billing" }},
		{"overlapping route prefixes", func(c *Config) {
			c.Routes = append(c.Routes, Route{PublicPrefix: "/business/people", UpstreamPrefix: "/v2/people", Methods: []string{http.MethodGet}})
		}},
		{"unsafe method", func(c *Config) { c.Routes[0].Methods = []string{"CONNECT"} }},
		{"noncanonical prefix", func(c *Config) { c.Routes[0].PublicPrefix = "/business/%2e%2e/admin" }},
		{"case-insensitive reserved prefix", func(c *Config) { c.Routes[0].PublicPrefix = "/BILLING" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := Config{Origin: base.Origin, Routes: append([]Route(nil), base.Routes...)}
			candidate.Routes[0].Methods = append([]string(nil), base.Routes[0].Methods...)
			test.mutate(&candidate)
			if _, err := NewRequestBuilder(candidate); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("NewRequestBuilder() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestRequestBuilderAllowsDisjointMethodsOnSamePath(t *testing.T) {
	builder, err := NewRequestBuilder(Config{
		Origin: "https://business.invalid",
		Routes: []Route{
			{PublicPrefix: "/business/people", UpstreamPrefix: "/v1/read", Methods: []string{http.MethodGet}},
			{PublicPrefix: "/business/people", UpstreamPrefix: "/v1/write", Methods: []string{http.MethodPost}, RequestContentTypes: []string{"application/json"}},
		},
	})
	if err != nil {
		t.Fatalf("NewRequestBuilder(): %v", err)
	}
	for _, test := range []struct{ method, path string }{
		{method: http.MethodGet, path: "/v1/read"},
		{method: http.MethodPost, path: "/v1/write"},
	} {
		request := requestWithPath(test.method, "/business/people")
		if test.method == http.MethodPost {
			request.Body = io.NopCloser(strings.NewReader(`{}`))
			request.ContentLength = 2
			request.Header.Set("Content-Type", "application/json")
		}
		out, cancel, err := builder.Build(request)
		if err != nil {
			t.Fatalf("Build(%s): %v", test.method, err)
		}
		if out.URL.Path != test.path {
			t.Errorf("Build(%s) path = %q, want %q", test.method, out.URL.Path, test.path)
		}
		cancel()
	}
}

func TestRequestFilteringAppliesUpstreamTimeout(t *testing.T) {
	builder := mustBuilder(t, "https://business.invalid")
	request := requestWithPath(http.MethodGet, "/business/people")
	out, cancel, err := builder.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	deadline, ok := out.Context().Deadline()
	if !ok {
		t.Fatal("outbound request has no timeout")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > maxUpstreamTimeout {
		t.Fatalf("outbound deadline remaining = %s, want (0, %s]", remaining, maxUpstreamTimeout)
	}
}

func mustBuilder(t *testing.T, origin string) *RequestBuilder {
	t.Helper()
	builder, err := NewRequestBuilder(Config{
		Origin: origin,
		Routes: []Route{{
			PublicPrefix:        "/business",
			UpstreamPrefix:      "/v1/customers",
			Methods:             []string{http.MethodGet, http.MethodPost},
			QueryKeys:           []QueryKey{{Name: "filter"}},
			RequestContentTypes: []string{"application/json"},
		}},
	})
	if err != nil {
		t.Fatalf("NewRequestBuilder(): %v", err)
	}
	return builder
}

func requestWithPath(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("X-AMOS-Identity", "forged")
	request.Header.Set("X-Tenant-ID", "forged")
	return request
}

func writeBuildError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, ErrMethodNotAllowed) {
		status = http.StatusMethodNotAllowed
	} else if errors.Is(err, ErrBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, "request rejected", status)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
