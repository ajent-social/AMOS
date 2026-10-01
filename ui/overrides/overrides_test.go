package overrides

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ajent-social/amos/ui/render"
)

const validPageTemplate = `{{template "amos:head" .}}<main><h1>{{.View.Heading}}</h1>{{template "amos:secure-form" .}}</main>`

func defaultRenderer(t *testing.T) render.Renderer {
	t.Helper()
	d, err := render.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newTestRenderer(t *testing.T, source string) (*Renderer, error) {
	t.Helper()
	return New(defaultRenderer(t), Config{
		ContractVersion: render.ContractVersion,
		Files:           fstest.MapFS{"signin.html": &fstest.MapFile{Data: []byte(source)}, "theme.css": &fstest.MapFile{Data: []byte("body { color: #123; }")}},
		Theme:           Theme{Namespace: "sample", Tokens: map[string]string{"accent": "#123456"}, Assets: []Asset{{Path: "site.css", File: "theme.css", ContentType: "text/css"}}},
		Pages:           []PageOverride{{Page: render.Page{ID: "signin", Version: render.ContractVersion}, Template: "signin.html"}},
	})
}

func TestStartupRejectsMissingSecuritySlot(t *testing.T) {
	_, err := newTestRenderer(t, `{{template "amos:head" .}}<main>{{.View.Heading}}</main>`)
	if !errors.Is(err, ErrMissingSlot) {
		t.Fatalf("New error = %v, want ErrMissingSlot", err)
	}
}

func TestStartupRejectsDisabledSecuritySlot(t *testing.T) {
	_, err := newTestRenderer(t, `{{template "amos:head" .}}<main><!-- {{template "amos:secure-form" .}} --></main>`)
	if !errors.Is(err, ErrMissingSlot) {
		t.Fatalf("New error = %v, want ErrMissingSlot for commented-out form", err)
	}
}

func TestStartupRejectsUnsupportedVersion(t *testing.T) {
	_, err := New(defaultRenderer(t), Config{ContractVersion: "0.9", Files: fstest.MapFS{}, Theme: Theme{Namespace: "sample"}})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New error = %v, want ErrInvalidConfig", err)
	}
}

func TestStartupRequiresDefaultRenderer(t *testing.T) {
	_, err := New(nil, Config{ContractVersion: render.ContractVersion, Files: fstest.MapFS{}, Theme: Theme{Namespace: "sample"}, Pages: []PageOverride{{Page: render.Page{ID: "signin", Version: render.ContractVersion}, Template: "signin.html"}}})
	if !errors.Is(err, ErrMissingDefault) {
		t.Fatalf("New error = %v, want ErrMissingDefault", err)
	}
}

func TestRenderEscapesTextAndKeepsSecureFormContract(t *testing.T) {
	r, err := newTestRenderer(t, validPageTemplate)
	if err != nil {
		t.Fatal(err)
	}
	model := render.ViewModel{Title: "Sign in", Heading: `<img src=x onerror=alert(1)>`, Form: &render.Form{Action: "/signin", Method: "POST", Label: "Email", Field: "email", Value: `a&b@example.test`, Submit: "Continue", CSRFToken: `token"<>&`}}
	w := httptest.NewRecorder()
	err = r.Render(context.Background(), w, httptest.NewRequest("GET", "/signin", nil), render.Page{ID: "signin", Version: render.ContractVersion}, model)
	if err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	for _, want := range []string{`&lt;img src=x onerror=alert(1)&gt;`, `name="_csrf"`, `value="token&#34;&lt;&gt;&amp;"`, `action="/signin"`, `data-amos-action-contract="1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `<img src=x`) {
		t.Fatalf("unescaped content: %s", body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "form-action 'self'") || w.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Fatalf("missing response security policy: %v", w.Header())
	}
}

func TestRenderFailsClosedWithoutCSRF(t *testing.T) {
	r, err := newTestRenderer(t, validPageTemplate)
	if err != nil {
		t.Fatal(err)
	}
	model := render.ViewModel{Title: "Sign in", Heading: "Sign in", Form: &render.Form{Action: "/signin", Method: "POST", Label: "Email", Field: "email", Submit: "Continue"}}
	w := httptest.NewRecorder()
	err = r.Render(context.Background(), w, httptest.NewRequest("GET", "/signin", nil), render.Page{ID: "signin", Version: render.ContractVersion}, model)
	if !errors.Is(err, ErrInvalidView) {
		t.Fatalf("Render error = %v, want ErrInvalidView", err)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("unsafe response body was written: %q", w.Body.String())
	}
}

func TestThemeAssetsAreExactAndImmutable(t *testing.T) {
	r, err := newTestRenderer(t, validPageTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var cssPath string
	for p := range r.assets {
		if strings.HasSuffix(p, "/site.css") {
			cssPath = p
		}
	}
	if cssPath == "" {
		t.Fatal("versioned site stylesheet missing")
	}
	w := httptest.NewRecorder()
	r.AssetHandler().ServeHTTP(w, httptest.NewRequest("GET", cssPath, nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || !bytes.Contains(w.Body.Bytes(), []byte("body {")) {
		t.Fatalf("asset response: code=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	missing := httptest.NewRecorder()
	r.AssetHandler().ServeHTTP(missing, httptest.NewRequest("GET", "/etc/passwd", nil))
	if missing.Code != 404 {
		t.Fatalf("unlisted asset status = %d", missing.Code)
	}
}

func TestUnselectedPageUsesDefault(t *testing.T) {
	r, err := newTestRenderer(t, validPageTemplate)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	model := render.ViewModel{Title: "Dashboard", Heading: "Default dashboard"}
	err = r.Render(context.Background(), w, httptest.NewRequest("GET", "/dashboard", nil), render.Page{ID: "dashboard", Version: render.ContractVersion}, model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "Default dashboard") || strings.Contains(w.Body.String(), "sample") {
		t.Fatalf("default rendering not used: %s", w.Body.String())
	}
}

func TestThemeRejectsActiveCSS(t *testing.T) {
	_, err := New(defaultRenderer(t), Config{ContractVersion: render.ContractVersion, Files: fstest.MapFS{"evil.css": &fstest.MapFile{Data: []byte("body{background:url(https://evil.test/x)}")}}, Theme: Theme{Namespace: "sample", Assets: []Asset{{Path: "evil.css", File: "evil.css", ContentType: "text/css"}}}, Pages: []PageOverride{{Page: render.Page{ID: "signin", Version: render.ContractVersion}, Template: "missing"}}})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New error = %v, want ErrInvalidConfig", err)
	}
}
