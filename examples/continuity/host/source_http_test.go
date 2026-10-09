package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSourceHTTPSelectors(t *testing.T) {
	id := "01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"list", sourcePath, 0},
		{"detail", sourcePath + "/" + id, 0},
		{"query", sourcePath + "?q=literal%25&kind=document&limit=100&after=" + id, 0},
		{"unknown", "/outside", 404},
		{"alias", sourcePath + "/../sources", 400},
		{"uppercaseID", sourcePath + "/019a0000-0000-7000-8000-000000000001", 0},
		{"invalidID", sourcePath + "/019A0000-0000-7000-8000-000000000001", 400},
		{"encodedPath", "/continuity/%73ources", 400},
		{"foreignSelector", sourcePath + "?workspace=" + id, 400},
		{"duplicate", sourcePath + "?q=one&q=two", 400},
		{"badEscape", sourcePath + "?q=%zz", 400},
		{"semicolon", sourcePath + "?q=x;y", 400},
		{"kind", sourcePath + "?kind=anything", 400},
		{"control", sourcePath + "?q=%0ax", 400},
		{"invalidUTF8", sourcePath + "?q=%ff", 400},
		{"onlySpace", sourcePath + "?q=+", 400},
		{"tooLong", sourcePath + "?q=" + strings.Repeat("a", 121), 400},
		{"tooManyBytes", sourcePath + "?q=" + strings.Repeat("a", 481), 400},
		{"queryBudget", sourcePath + "?q=" + strings.Repeat("a", 2049), 400},
		{"paddedLimit", sourcePath + "?limit=01", 400},
		{"zeroLimit", sourcePath + "?limit=0", 400},
		{"emptyLimit", sourcePath + "?limit=", 400},
		{"negativeLimit", sourcePath + "?limit=-1", 400},
		{"overflowLimit", sourcePath + "?limit=999999999999999999999", 400},
		{"detailQuery", sourcePath + "/" + id + "?q=x", 400},
		{"detailQuestion", sourcePath + "/" + id + "?", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			_, status := sourceInput(r)
			if status != tc.status {
				t.Fatalf("status=%d want=%d", status, tc.status)
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, sourcePath+"?q=+literal%25+", nil)
	q, status := sourceInput(r)
	if status != 0 || q.Query != "literal%" || q.Limit != 25 || q.WorkspaceID != uuid.Nil {
		t.Fatalf("default personal selection/normalization: %#v status %d", q, status)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodOptions, http.MethodDelete} {
		r.Method = method
		if _, status := sourceInput(r); status != http.StatusMethodNotAllowed {
			t.Fatalf("admitted unsupported %s", method)
		}
	}
}

func TestSourceHTTPRejectsUnsupportedRequestShapes(t *testing.T) {
	for _, alter := range []func(*http.Request){
		func(r *http.Request) { r.ContentLength = 1 },
		func(r *http.Request) { r.ContentLength = -1 },
		func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.URL.User = url.User("untrusted") },
		func(r *http.Request) { r.URL.Host = "example.invalid" },
		func(r *http.Request) { r.URL.Scheme = "https" },
		func(r *http.Request) { r.URL.Fragment = "fragment" },
		func(r *http.Request) { r.URL.Path = strings.Repeat("x", 129) },
		func(r *http.Request) { r.URL = nil },
	} {
		r := httptest.NewRequest(http.MethodGet, sourcePath, nil)
		alter(r)
		if _, status := sourceInput(r); status != http.StatusBadRequest {
			t.Fatalf("unsupported request admitted: %d", status)
		}
	}
}

func TestSourceHTTPFragmentFallback(t *testing.T) {
	for _, tc := range []struct {
		request, target []string
		fragment        bool
	}{
		{nil, nil, false},
		{[]string{"true"}, []string{"continuity-source-content"}, true},
		{[]string{"true"}, nil, false},
		{nil, []string{"continuity-source-content"}, false},
		{[]string{"true", "true"}, []string{"continuity-source-content"}, false},
		{[]string{"true"}, []string{"continuity-source-content", "continuity-source-content"}, false},
		{[]string{"false"}, []string{"continuity-source-content"}, false},
		{[]string{"true"}, []string{"foreign-target"}, false},
	} {
		h := make(http.Header)
		for _, v := range tc.request {
			h.Add("HX-Request", v)
		}
		for _, v := range tc.target {
			h.Add("HX-Target", v)
		}
		if got := sourceFragment(h); got != tc.fragment {
			t.Fatalf("fragment=%v for %#v", got, h)
		}
	}
}

func sourceTestResponse(t *testing.T) *sourceResponse {
	t.Helper()
	b := &sourceResponse{header: make(http.Header), committed: true}
	b.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := b.Write([]byte("PROTECTED SOURCE")); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSourceHTTPPublicationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*sourceResponse, *http.Request)
	}{
		{"notCommitted", func(b *sourceResponse, _ *http.Request) { b.committed = false }},
		{"bodyFailure", func(b *sourceResponse, _ *http.Request) { b.failed = true }},
		{"redirect", func(b *sourceResponse, _ *http.Request) { b.status = 302 }},
		{"cookie", func(b *sourceResponse, _ *http.Request) { b.header.Add("Set-Cookie", "bad=value") }},
		{"largeHeader", func(b *sourceResponse, _ *http.Request) { b.header.Set("X-Large", strings.Repeat("x", 8193)) }},
		{"manyHeaders", func(b *sourceResponse, _ *http.Request) { b.header["X-Many"] = make([]string, 33) }},
		{"duplicateType", func(b *sourceResponse, _ *http.Request) { b.header.Add("Content-Type", "text/html; charset=utf-8") }},
		{"empty", func(b *sourceResponse, _ *http.Request) { b.body.Reset() }},
		{"canceled", func(_ *sourceResponse, r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			*r = *r.WithContext(ctx)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := sourceTestResponse(t)
			r := httptest.NewRequest(http.MethodGet, sourcePath, nil)
			w := httptest.NewRecorder()
			tc.change(b, r)
			if w.Body.Len() != 0 {
				t.Fatal("provisional body escaped")
			}
			b.publish(w, r, "server-id", false)
			if w.Code != 503 || strings.Contains(w.Body.String(), "PROTECTED SOURCE") || w.Header().Get("Set-Cookie") != "" {
				t.Fatalf("failed completion disclosed source: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		b := sourceTestResponse(t)
		w := httptest.NewRecorder()
		b.publish(w, httptest.NewRequest(method, sourcePath, nil), "server-id", true)
		if w.Code != 200 || w.Header().Get("Content-Length") != strconv.Itoa(len("PROTECTED SOURCE")) || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") != "server-id" || w.Header().Get("Vary") != "HX-Request, HX-Target" {
			t.Fatalf("bad completed response: %d %#v", w.Code, w.Header())
		}
		if method == http.MethodHead && w.Body.Len() != 0 || method == http.MethodGet && w.Body.String() != "PROTECTED SOURCE" {
			t.Fatal("GET/HEAD publication mismatch")
		}
	}
}

func TestSourceHTTPCollectorBoundsAndErrorSanitization(t *testing.T) {
	b := &sourceResponse{header: make(http.Header)}
	if _, ok := any(b).(http.Flusher); ok {
		t.Fatal("collector exposes flushing")
	}
	if _, ok := any(b).(interface{ Unwrap() http.ResponseWriter }); ok {
		t.Fatal("collector exposes underlying writer")
	}
	if n, err := b.Write(make([]byte, sourceHTTPMax)); err != nil || n != sourceHTTPMax {
		t.Fatal("exact body bound rejected")
	}
	if _, err := b.Write([]byte{1}); err == nil || !b.failed || b.body.Len() != 0 {
		t.Fatal("overflow did not discard provisional bytes")
	}
	if _, err := b.Write([]byte("later")); err == nil {
		t.Fatal("overflow poisoning lost")
	}
	for _, status := range []int{400, 401, 403, 404, 405, 503} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			b := &sourceResponse{header: make(http.Header), status: status}
			b.Header().Set("X-Request-ID", "untrusted-middleware-id")
			if _, err := b.Write([]byte("PRIVATE DIAGNOSTIC")); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			b.publish(w, httptest.NewRequest(method, sourcePath, nil), "server-id", true)
			if w.Code != status || strings.Contains(w.Body.String(), "PRIVATE DIAGNOSTIC") || w.Header().Get("X-Request-ID") != "server-id" || strings.Contains(w.Body.String(), "<html") {
				t.Fatal("middleware error not sanitized")
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD error has body")
			}
			if status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("method response lacks allow")
			}
		}
	}
}

func TestSourceHTTPPanicDiscardsProvisionalOutput(t *testing.T) {
	b := sourceTestResponse(t)
	b.collect(func() { panic("PRIVATE DIAGNOSTIC") })
	w := httptest.NewRecorder()
	w.Header().Set("Set-Cookie", "prior=value")
	w.Header().Set("Location", "/prior")
	b.publish(w, httptest.NewRequest(http.MethodGet, sourcePath, nil), "server-id", false)
	if w.Code != 503 || strings.Contains(w.Body.String(), "PROTECTED SOURCE") || strings.Contains(w.Body.String(), "PRIVATE DIAGNOSTIC") || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
		t.Fatal("panic disclosed provisional response")
	}
}

func TestSourceHTTPUnavailableCoreAndCorrelation(t *testing.T) {
	var c *readCore
	ids := map[string]bool{}
	for _, path := range []string{sourcePath, sourcePath + "?unknown=1"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Request-ID", "caller-controlled")
		w := httptest.NewRecorder()
		c.sourceHandler().ServeHTTP(w, r)
		id := w.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil || ids[id] || id == "caller-controlled" || !strings.Contains(w.Body.String(), id) {
			t.Fatal("missing/untrusted/reused correlation")
		}
		ids[id] = true
		want := 503
		if strings.Contains(path, "?") {
			want = 400
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
	}
}
