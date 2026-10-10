package host

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Synthetic scoped setup and native middleware exercise only these private GET
// and HEAD routes. No listener, POST, failed-commit schedule or signup is tested.
func TestPrivateBaselineHTTPRuntimeRequiredService(t *testing.T) {
	f := privateBaselineFixture(t)
	if f == nil {
		return
	}
	handler := f.core.baselineHandler()
	request := func(method, path string, cookie, fragment bool, ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil).WithContext(ctx)
		r.Host = "reader.example.test"
		if cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: f.token})
		}
		if fragment {
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "continuity-baseline-content")
		}
		r.Header.Set("X-Request-ID", "caller-controlled")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	seen := map[string]bool{}
	check := func(t *testing.T, w *httptest.ResponseRecorder, status int, head, fragment bool) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("baseline HTTP status=%d want=%d", w.Code, status)
		}
		id := w.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil || seen[id] || id == "caller-controlled" {
			t.Error("missing or reused server correlation")
		}
		seen[id] = true
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
			t.Error("HTTP security headers missing or unsafe")
		}
		n, err := strconv.Atoi(w.Header().Get("Content-Length"))
		if err != nil || n < 1 {
			t.Error("missing representation length")
		}
		if head {
			if w.Body.Len() != 0 {
				t.Error("HEAD published a body")
			}
			return
		}
		b := w.Body.String()
		if n != w.Body.Len() || fragment == strings.Contains(b, "<html") || !strings.Contains(b, `id="continuity-baseline-content"`) {
			t.Error("HTTP representation mismatch")
		}
		if strings.Contains(b, "<script>") {
			t.Error("stored executable-looking data was not escaped")
		}
		for _, id := range []uuid.UUID{f.foreign.property, f.foreign.caseID, f.foreign.application, f.foreign.procedure, f.foreign.source} {
			if strings.Contains(b, id.String()) {
				t.Error("foreign record disclosed")
			}
		}
	}
	var nativeToken string
	// Obtain the comparison token through the same admitted native service, never
	// by parsing or deriving the credential in application/test helper code.
	f.core.sessions.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ok bool
		nativeToken, ok = f.core.sessions.CSRFToken(r)
		if !ok {
			t.Fatal("native form token unavailable")
		}
	})).ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, baselinePath, nil).WithContext(f.ctx)
		r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: f.token})
		return r
	}())
	if nativeToken == "" {
		t.Fatal("comparison token request was not admitted")
	}
	for _, tc := range []struct {
		name, path, want string
		form             bool
	}{
		{"properties", baselinePath + "?q=50%25_", f.own.property.String(), false},
		{"property", baselinePath + "/" + f.own.property.String(), "Synthetic occupant", false},
		{"cases", "/continuity/cases", f.own.caseID.String(), false},
		{"case", "/continuity/cases/" + f.own.caseID.String(), "No saved draft was supplied.", true},
		{"applications", "/continuity/applications", f.own.application.String(), false},
		{"application", "/continuity/applications/" + f.own.application.String(), "Final human decision is disabled", true},
		{"procedures", "/continuity/procedures", f.own.procedure.String(), false},
		{"procedure", "/continuity/procedures/" + f.own.procedure.String(), "&lt;script&gt;procedure&lt;/script&gt;", true},
		{"activity", "/continuity/activity", f.own.caseID.String(), false},
		{"guide default", "/continuity/guide", "Open cases in the supplied selection", false},
		{"guide handover", "/continuity/guide?topic=handover&case_id=&limit=1", "This bounded briefing", false},
		{"guide case", "/continuity/guide?topic=explain_case&case_id=" + f.own.caseID.String(), "Case explanation", false},
		{"guide authority", "/continuity/guide?topic=spending_authority&case_id=", "This response grants no authority.", false},
		{"guide draft", "/continuity/guide?topic=owner_draft&case_id=" + f.own.caseID.String(), "This preview has not been saved or sent.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fragment := range []bool{false, true} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					w := request(method, tc.path, true, fragment, f.ctx)
					check(t, w, 200, method == http.MethodHead, fragment)
					if method == http.MethodHead {
						continue
					}
					if !strings.Contains(w.Body.String(), tc.want) {
						t.Error("HTTP page omitted its stored content")
					}
					if tc.form {
						if !strings.Contains(w.Body.String(), `name="_csrf" value="`+nativeToken+`"`) {
							t.Error("HTTP detail did not use same native token")
						}
					} else if strings.Contains(w.Body.String(), `name="_csrf"`) {
						t.Error("non-form page received CSRF")
					}
				}
			}
		})
	}
	t.Run("stored saved draft remains stale", func(t *testing.T) {
		f.exec(t, `INSERT INTO public.continuity_drafts(installation_id,application_id,environment_id,workspace_id,case_id,case_revision,body,source_ids) VALUES($1,$2,$3,$4,$5,1,$6,$7)`, f.args(f.own.workspace, f.own.caseID, "Stored <script>draft</script>", f.marshal([]string{f.draftSource.String()}))...)
		f.exec(t, `UPDATE public.continuity_cases SET revision=2 WHERE `+baselineRuntimeScoped+` AND id=$5`, f.args(f.own.workspace, f.own.caseID)...)
		w := request(http.MethodGet, "/continuity/cases/"+f.own.caseID.String(), true, true, f.ctx)
		check(t, w, 200, false, true)
		for _, want := range []string{"Saved against case revision 1.", "The case changed after this draft was saved", `name="expected" value="2"`, "Stored &lt;script&gt;draft&lt;/script&gt;"} {
			if !strings.Contains(w.Body.String(), want) {
				t.Error("HTTP stale draft lost stored revision/body")
			}
		}
	})
	t.Run("missing and foreign details", func(t *testing.T) {
		for _, tc := range []struct {
			path    string
			foreign uuid.UUID
		}{{baselinePath, f.foreign.property}, {"/continuity/cases", f.foreign.caseID}, {"/continuity/applications", f.foreign.application}, {"/continuity/procedures", f.foreign.procedure}} {
			for _, id := range []uuid.UUID{testID(t), tc.foreign} {
				w := request(http.MethodGet, tc.path+"/"+id.String(), true, true, f.ctx)
				check(t, w, 404, false, true)
				if strings.Contains(w.Body.String(), f.own.caseID.String()) || !strings.Contains(w.Body.String(), "Continuity record not found.") {
					t.Error("error disclosed provisional records")
				}
			}
		}
	})
	t.Run("missing credential and invalid selector", func(t *testing.T) {
		w := request(http.MethodGet, baselinePath, false, true, f.ctx)
		check(t, w, 401, false, true)
		w = request(http.MethodGet, baselinePath+"?workspace="+f.own.workspace.String(), true, true, f.ctx)
		check(t, w, 400, false, true)
		w = request(http.MethodPost, baselinePath, true, true, f.ctx)
		check(t, w, 405, false, true)
		if w.Header().Get("Allow") != "GET, HEAD" {
			t.Error("POST rejection lacks Allow")
		}
	})
	t.Run("canceled original HTTP request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		w := request(http.MethodGet, "/continuity/cases", true, true, ctx)
		check(t, w, 503, false, true)
		if bytes.Contains(w.Body.Bytes(), []byte(f.own.caseID.String())) {
			t.Error("canceled request disclosed case")
		}
	})
	t.Run("stored draft dependency failure suppresses HTTP page", func(t *testing.T) {
		f.exec(t, `DELETE FROM public.continuity_sources WHERE `+baselineRuntimeScoped+` AND id=$5`, f.args(f.own.workspace, f.draftSource)...)
		w := request(http.MethodGet, "/continuity/cases/"+f.own.caseID.String(), true, true, f.ctx)
		check(t, w, 503, false, true)
		if strings.Contains(w.Body.String(), "Stored") || strings.Contains(w.Body.String(), "No saved draft") || strings.Contains(w.Body.String(), f.own.caseID.String()) {
			t.Error("draft dependency failure published partial detail")
		}
	})
	t.Run("stored source digest corruption suppresses HTTP page", func(t *testing.T) {
		f.exec(t, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+baselineRuntimeScoped+` AND id=$5`, f.args(f.own.workspace, f.own.source, strings.Repeat("0", 64))...)
		for _, path := range []string{"/continuity/cases", "/continuity/cases/" + f.own.caseID.String(), "/continuity/guide"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := request(method, path, true, true, f.ctx)
				check(t, w, 503, method == http.MethodHead, true)
				if strings.Contains(w.Body.String(), f.own.caseID.String()) || strings.Contains(w.Body.String(), f.own.source.String()) {
					t.Error("corrupt source published dependent records")
				}
			}
		}
	})
}
