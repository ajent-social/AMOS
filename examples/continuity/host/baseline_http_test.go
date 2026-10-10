package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/google/uuid"
)

func TestBaselineHTTPSelectors(t *testing.T) {
	const id = "019a0000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		path   string
		page   baselinePage
		detail bool
	}{
		{baselinePath, pageProperties, false}, {baselinePath + "/" + id, pageProperty, true},
		{"/continuity/cases", pageCases, false}, {"/continuity/cases/" + id, pageCase, true},
		{"/continuity/applications", pageApplications, false}, {"/continuity/applications/" + id, pageApplication, true},
		{"/continuity/procedures", pageProcedures, false}, {"/continuity/procedures/" + id, pageProcedure, true},
		{"/continuity/activity", pageActivity, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				q, status := baselineInput(httptest.NewRequest(method, tc.path, nil))
				if status != 0 || q.Page != tc.page || q.WorkspaceID != uuid.Nil || q.CSRFToken != "" || q.Fragment {
					t.Fatalf("bad selector %#v status %d", q, status)
				}
				if tc.detail {
					if q.ID != id || q.Limit != 0 {
						t.Fatal("detail acquired list defaults")
					}
					for _, suffix := range []string{"?", "?limit=1", "?q=", "?after=", "?topic=attention", "?case_id=", "?x=1"} {
						if _, s := baselineInput(httptest.NewRequest(method, tc.path+suffix, nil)); s != 400 {
							t.Errorf("detail accepted query %q", suffix)
						}
					}
				} else {
					if q.Limit != 25 || q.ID != "" {
						t.Fatal("list defaults incorrect")
					}
					q, status = baselineInput(httptest.NewRequest(method, tc.path+"?after="+id+"&limit=100", nil))
					if status != 0 || q.After != id || q.Limit != 100 {
						t.Fatal("list cursor/limit rejected")
					}
					for _, key := range []string{"q", "topic", "case_id", "workspace", "actor", "principal", "_csrf"} {
						if key == "q" && tc.page == pageProperties {
							continue
						}
						if _, s := baselineInput(httptest.NewRequest(method, tc.path+"?"+key+"=", nil)); s != 400 {
							t.Errorf("accepted unsupported empty %s", key)
						}
					}
				}
			}
		})
	}
	for _, value := range []string{"", " ", "\u2003", " literal%_ ", strings.Repeat("界", 120)} {
		q, status := baselineInput(httptest.NewRequest(http.MethodGet, baselinePath+"?q="+url.QueryEscape(value), nil))
		if status != 0 || q.Query != strings.TrimSpace(value) {
			t.Fatalf("property normalization failed for %q", value)
		}
	}
}

func TestBaselineHTTPGuideSelectors(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		query  string
		topic  guide.Topic
		caseID string
		limit  int
	}{
		{"", guide.Attention, "", 25}, {"?", guide.Attention, "", 25},
		{"?case_id=", guide.Attention, "", 25},
		{"?topic=attention&case_id=&limit=1", guide.Attention, "", 1},
		{"?topic=handover", guide.Handover, "", 25},
		{"?topic=handover&limit=100&case_id=", guide.Handover, "", 100},
		{"?topic=explain_case&case_id=" + id, guide.ExplainCase, id, 0},
		{"?topic=owner_draft&case_id=" + id, guide.OwnerDraft, id, 0},
		{"?topic=spending_authority", guide.SpendingAuthority, "", 0},
		{"?topic=spending_authority&case_id=", guide.SpendingAuthority, "", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			q, status := baselineInput(httptest.NewRequest(http.MethodGet, "/continuity/guide"+tc.query, nil))
			if status != 0 || q.Page != pageGuide || q.Topic != tc.topic || q.CaseID != tc.caseID || q.Limit != tc.limit || q.CSRFToken != "" {
				t.Fatalf("bad guide %#v status=%d", q, status)
			}
			if _, ok := normalizeBaselineQuery(q); !ok {
				t.Fatal("parsed guide rejected by private reader")
			}
		})
	}
	for _, query := range []string{
		"topic=", "topic=unknown", "topic=Attention", "topic=" + strings.Repeat("x", 19),
		"topic=attention&topic=attention", "case_id=&case_id=", "limit=1&limit=1",
		"case_id=" + id, "topic=handover&case_id=" + id,
		"topic=spending_authority&case_id=" + id, "topic=spending_authority&limit=25",
		"topic=explain_case", "topic=explain_case&case_id=", "topic=owner_draft&case_id=",
		"topic=explain_case&case_id=" + id + "&limit=1", "topic=owner_draft&case_id=" + id + "&limit=25",
		"topic=owner_draft&case_id=invalid", "topic=owner_draft&case_id=" + strings.Repeat("a", 37),
		"topic=explain_case&case_id=019A0000-0000-7000-8000-000000000001",
		"q=", "after=", "_csrf=", "workspace=", "topic=%ff", "topic=%zz", "topic=x;y",
	} {
		t.Run(query, func(t *testing.T) {
			if _, status := baselineInput(httptest.NewRequest(http.MethodGet, "/continuity/guide?"+query, nil)); status != 400 {
				t.Fatalf("guide accepted invalid selector: %d", status)
			}
		})
	}
}

func TestBaselineHTTPRejectsUnsupportedShapes(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	for _, path := range []string{
		baselinePath + "/", baselinePath + "/" + id + "/extra", baselinePath + "/../properties",
		baselinePath + "/019A0000-0000-7000-8000-000000000001", baselinePath + "/invalid",
		"/continuity/%70roperties", "/continuity/activity/", "/continuity/guide/" + id,
		baselinePath + "?q=%zz", baselinePath + "?q=x;y", baselinePath + "?q=%ff",
		baselinePath + "?q=%0a", baselinePath + "?q=" + strings.Repeat("a", 121),
		baselinePath + "?q=" + strings.Repeat("a", 481), baselinePath + "?q=" + strings.Repeat("a", 2049),
		baselinePath + "?q=x&q=x", baselinePath + "?after=&after=", baselinePath + "?after=invalid",
		baselinePath + "?after=" + strings.Repeat("a", 37), baselinePath + "?limit=1&limit=1",
	} {
		t.Run(path, func(t *testing.T) {
			if _, status := baselineInput(httptest.NewRequest(http.MethodGet, path, nil)); status != 400 {
				t.Fatalf("unsupported selector admitted: %d", status)
			}
		})
	}
	for _, route := range []string{baselinePath, "/continuity/cases", "/continuity/applications", "/continuity/procedures", "/continuity/activity", "/continuity/guide"} {
		for _, limit := range []string{"", "0", "101", "01", "-1", "+1", " 1", "1.0", "99999999999999999"} {
			if _, status := baselineInput(httptest.NewRequest(http.MethodGet, route+"?limit="+url.QueryEscape(limit), nil)); status != 400 {
				t.Errorf("accepted limit %q on %s", limit, route)
			}
		}
	}
	for _, path := range []string{"/outside", "/continuity", "/continuity/source", "/continuity/properties-alias", "/continuity/sources"} {
		if _, status := baselineInput(httptest.NewRequest(http.MethodGet, path, nil)); status != 404 {
			t.Errorf("unknown path status %d", status)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace} {
		if _, status := baselineInput(httptest.NewRequest(method, baselinePath, nil)); status != 405 {
			t.Errorf("method %s admitted", method)
		}
	}
	for _, alter := range []func(*http.Request){
		func(r *http.Request) { r.ContentLength = 1 }, func(r *http.Request) { r.ContentLength = -1 },
		func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.URL.User = url.User("untrusted") }, func(r *http.Request) { r.URL.Host = "example.invalid" },
		func(r *http.Request) { r.URL.Scheme = "https" }, func(r *http.Request) { r.URL.Fragment = "fragment" },
		func(r *http.Request) { r.URL.Path = strings.Repeat("x", 129) }, func(r *http.Request) { r.URL.RawPath = baselinePath },
		func(r *http.Request) { r.URL = nil },
	} {
		r := httptest.NewRequest(http.MethodGet, baselinePath, nil)
		alter(r)
		if _, status := baselineInput(r); status != 400 {
			t.Errorf("unsupported transport admitted: %d", status)
		}
	}
}

func TestBaselineHTTPFragmentAndTokenExclusion(t *testing.T) {
	for _, tc := range []struct {
		request, target []string
		want            bool
	}{
		{nil, nil, false}, {[]string{"true"}, []string{"continuity-baseline-content"}, true},
		{[]string{"true"}, nil, false}, {nil, []string{"continuity-baseline-content"}, false},
		{[]string{"true", "true"}, []string{"continuity-baseline-content"}, false},
		{[]string{"true"}, []string{"continuity-baseline-content", "continuity-baseline-content"}, false},
		{[]string{"false"}, []string{"continuity-baseline-content"}, false},
		{[]string{"true"}, []string{"continuity-source-content"}, false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/continuity/cases/01900000-0000-7000-8000-000000000001", nil)
		for _, v := range tc.request {
			r.Header.Add("HX-Request", v)
		}
		for _, v := range tc.target {
			r.Header.Add("HX-Target", v)
		}
		r.Header.Set("X-CSRF-Token", "caller-controlled")
		r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: "caller-controlled"})
		q, status := baselineInput(r)
		if status != 0 || q.Fragment != tc.want || q.CSRFToken != "" {
			t.Fatalf("transport invented token or fragment %#v", q)
		}
	}
}

func TestBaselineHTTPPublicationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*sourceResponse, *http.Request)
	}{
		{"uncommitted", func(b *sourceResponse, _ *http.Request) { b.committed = false }},
		{"failed", func(b *sourceResponse, _ *http.Request) { b.failed = true }},
		{"unknownPresentation", func(b *sourceResponse, _ *http.Request) { b.presentation = readPresentation(255) }},
		{"largeHeader", func(b *sourceResponse, _ *http.Request) { b.header.Set("X-Large", strings.Repeat("x", 8193)) }},
		{"manyHeaders", func(b *sourceResponse, _ *http.Request) { b.header["X-Many"] = make([]string, 33) }},
		{"cookie", func(b *sourceResponse, _ *http.Request) { b.header.Set("Set-Cookie", "private=value") }},
		{"redirect", func(b *sourceResponse, _ *http.Request) { b.status = 302 }},
		{"duplicateType", func(b *sourceResponse, _ *http.Request) { b.header.Add("Content-Type", "text/html; charset=utf-8") }},
		{"empty", func(b *sourceResponse, _ *http.Request) { b.body.Reset() }},
		{"canceled", func(_ *sourceResponse, r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			*r = *r.WithContext(ctx)
		}},
		{"expired", func(_ *sourceResponse, r *http.Request) {
			ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
			defer cancel()
			*r = *r.WithContext(ctx)
		}},
		{"panic", func(b *sourceResponse, _ *http.Request) { b.collect(func() { panic("PRIVATE DIAGNOSTIC") }) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := sourceTestResponse(t)
			b.presentation = baselinePresentation
			r := httptest.NewRequest(http.MethodGet, baselinePath, nil)
			w := httptest.NewRecorder()
			tc.change(b, r)
			if w.Body.Len() != 0 {
				t.Fatal("provisional bytes escaped")
			}
			b.publish(w, r, "server-id", true)
			if w.Code != 503 || strings.Contains(w.Body.String(), "PROTECTED SOURCE") || strings.Contains(w.Body.String(), "PRIVATE DIAGNOSTIC") || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("failed completion disclosed output")
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		b := sourceTestResponse(t)
		b.presentation = baselinePresentation
		w := httptest.NewRecorder()
		b.publish(w, httptest.NewRequest(method, baselinePath, nil), "server-id", false)
		if w.Code != 200 || w.Header().Get("Content-Length") != strconv.Itoa(len("PROTECTED SOURCE")) {
			t.Fatal("committed GET/HEAD failed")
		}
		if method == http.MethodHead && w.Body.Len() != 0 || method == http.MethodGet && w.Body.String() != "PROTECTED SOURCE" {
			t.Fatal("HEAD/body publication mismatch")
		}
	}
}

func TestBaselineHTTPErrorPresentation(t *testing.T) {
	for _, presentation := range []readPresentation{sourcePresentation, baselinePresentation} {
		section, recovery := "continuity-source-content", "/continuity/sources"
		if presentation == baselinePresentation {
			section, recovery = "continuity-baseline-content", baselinePath
		}
		for _, status := range []int{400, 401, 403, 404, 405, 503} {
			for _, fragment := range []bool{false, true} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					b := &sourceResponse{header: make(http.Header), status: status, presentation: presentation}
					if _, err := b.Write([]byte("PRIVATE DIAGNOSTIC")); err != nil {
						t.Fatal(err)
					}
					w := httptest.NewRecorder()
					w.Header().Set("Set-Cookie", "prior=value")
					w.Header().Set("Location", "/private")
					w.Header().Set("ETag", "private")
					b.publish(w, httptest.NewRequest(method, baselinePath, nil), "server-id", fragment)
					if w.Code != status || strings.Contains(w.Body.String(), "PRIVATE DIAGNOSTIC") {
						t.Fatal("error status/diagnostic changed")
					}
					if method == http.MethodGet && (!strings.Contains(w.Body.String(), `id="`+section+`"`) || !strings.Contains(w.Body.String(), `href="`+recovery+`"`) || fragment == strings.Contains(w.Body.String(), "<html")) {
						t.Fatal("wrong error presentation")
					}
					if method == http.MethodHead && w.Body.Len() != 0 {
						t.Fatal("HEAD error has bytes")
					}
					if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Vary") != "HX-Request, HX-Target" || w.Header().Get("X-Request-ID") != "server-id" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" || w.Header().Get("ETag") != "" {
						t.Fatal("error security headers incorrect")
					}
					if status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
						t.Fatal("missing Allow header")
					}
				}
			}
		}
	}
	b := baselineErrorBody(503, `<script>untrusted</script>`, true)
	if strings.Contains(string(b), "<script>") || !strings.Contains(string(b), "&lt;script&gt;") {
		t.Fatal("error context not escaped")
	}
}

func TestBaselineHTTPUnavailableCoreAndCorrelation(t *testing.T) {
	var core *readCore
	seen := map[string]bool{}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, baselinePath, 503}, {http.MethodGet, baselinePath + "?unknown=", 400},
		{http.MethodGet, "/outside", 404}, {http.MethodPost, baselinePath, 405},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("X-Request-ID", "caller-controlled")
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Target", "continuity-baseline-content")
		w := httptest.NewRecorder()
		core.baselineHandler().ServeHTTP(w, r)
		id := w.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil || seen[id] || id == "caller-controlled" || !strings.Contains(w.Body.String(), id) {
			t.Fatal("invalid request correlation")
		}
		seen[id] = true
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `id="continuity-baseline-content"`) || strings.Contains(w.Body.String(), "<html") {
			t.Fatalf("baseline failure presentation/status incorrect %d", w.Code)
		}
	}
}
