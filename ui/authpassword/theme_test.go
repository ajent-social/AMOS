package authpassword

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	nethtml "golang.org/x/net/html"
)

func TestThemeRejectsNoncanonicalAssets(t *testing.T) {
	invalid := []string{"/", "relative.css", "https://example.test/a.css", "//example.test/a", "/a//b", "/a/", "/./a", "/a/../b", "/%2e%2e/a", "/a%2Fb", "/a?b", "/a#b", "/a\\b", "/a\nb", "/a b", "/é.css", "/a:foo", "/a\x00"}
	for _, asset := range invalid {
		t.Run(asset, func(t *testing.T) {
			for _, theme := range []Theme{{BrandName: "Example", StylesheetURL: asset}, {BrandName: "Example", LogoURL: asset}} {
				if !errors.Is(theme.Validate(), ErrInvalidTheme) {
					t.Fatalf("accepted noncanonical asset %q", asset)
				}
				p, _, _, _, _, _, _ := testPages(t)
				cfg := p.cfg
				cfg.Theme = &theme
				if _, err := New(cfg); !errors.Is(err, ErrInvalidTheme) {
					t.Fatal("constructor accepted noncanonical theme")
				}
			}
		})
	}
	for _, brand := range []string{"", "  \u2003", "x\n", "x\x7f", string([]byte{0xff}), strings.Repeat("é", 101)} {
		if !errors.Is((Theme{BrandName: brand}).Validate(), ErrInvalidTheme) {
			t.Fatalf("accepted invalid brand %q", brand)
		}
	}
	for _, theme := range []Theme{{BrandName: "Example"}, {BrandName: strings.Repeat("é", 100), LogoURL: "/assets/brand-1.svg", StylesheetURL: "/assets/theme.v1~ok.css"}} {
		if err := theme.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestThemeCopiedAndEscaped(t *testing.T) {
	base, _, _, _, _, _, _ := testPages(t)
	theme := Theme{BrandName: `<script>alert("brand")</script>`, LogoURL: "/assets/logo.svg", StylesheetURL: "/assets/theme.css"}
	cfg := base.cfg
	cfg.Theme = &theme
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	theme.BrandName, theme.LogoURL, theme.StylesheetURL = "Mutated", "//external.test/a", "/changed.css"
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/signin", nil))
	body := w.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "Mutated") || strings.Contains(body, "external.test") || strings.Contains(body, "changed.css") || !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, `href="/assets/theme.css"`) || !strings.Contains(body, `src="/assets/logo.svg" width="32" height="32" alt=""`) {
		t.Fatal("theme was not copied and escaped")
	}
}

func TestNilThemePreservesLegacySignin(t *testing.T) {
	p, _, _, _, _, _, _ := testPages(t)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/signin", nil))
	want, err := os.ReadFile("testdata/signin-legacy.html")
	if err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != string(want) || w.Header().Get("Content-Security-Policy") != "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'" {
		t.Fatal("nil theme changed legacy HTML or CSP")
	}
}

func TestThemeAcrossLifecycleOutcomes(t *testing.T) {
	id, token := uuid.NewString(), strings.Repeat("a", 43)
	query := "?challenge=" + id + "&token=" + token
	verifyBody := "challenge=" + id + "&token=" + token
	resetBody := url.Values{"challenge_id": {id}, "token": {token}, "password": {"synthetic-new-password"}}.Encode()
	credentialBody := url.Values{"email": {"a"}, "password": {"b"}}.Encode()
	signupBody := url.Values{"email": {"synthetic@example.test"}, "password": {"synthetic-password"}}.Encode()
	tests := []struct {
		name, method, path, body, origin string
		status, actionStatus             int
	}{
		{"signin", "GET", "/signin", "", "", 200, 200},
		{"signup", "GET", "/signup", "", "", 200, 200},
		{"recovery", "GET", "/forgot-password", "", "", 200, 200},
		{"verification", "GET", "/verify-email" + query, "", "", 200, 200},
		{"verification-invalid", "GET", "/verify-email", "", "", 200, 200},
		{"reset", "GET", "/reset-password" + query, "", "", 200, 200},
		{"reset-invalid", "GET", "/reset-password", "", "", 200, 200},
		{"head", "HEAD", "/verify-email" + query, "", "", 200, 200},
		{"signup-complete", "POST", "/signup", signupBody, "https://app.example", 202, 202},
		{"missing-field", "POST", "/signup", "email=synthetic%40example.test", "https://app.example", 200, 200},
		{"invalid-form", "POST", "/signup", "unknown=value", "https://app.example", 400, 200},
		{"wrong-origin", "POST", "/auth", credentialBody, "https://foreign.example", 403, 200},
		{"signin-denied", "POST", "/auth", credentialBody, "https://app.example", 200, 401},
		{"signin-unavailable", "POST", "/auth", credentialBody, "https://app.example", 503, 503},
		{"signin-throttled", "POST", "/auth", credentialBody, "https://app.example", 429, 429},
		{"recovery-complete", "POST", "/forgot-password", "email=a", "https://app.example", 200, 202},
		{"verify-complete", "POST", "/verify-email", verifyBody, "https://app.example", 200, 204},
		{"verify-unavailable", "POST", "/verify-email", verifyBody, "https://app.example", 503, 503},
		{"reset-password-invalid", "POST", "/reset-password", resetBody, "https://app.example", 200, 400},
		{"reset-unavailable", "POST", "/reset-password", resetBody, "https://app.example", 503, 503},
		{"reset-complete", "POST", "/reset-password", resetBody, "https://app.example", 303, 204},
		{"signin-complete", "POST", "/auth", credentialBody, "https://app.example", 303, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := func(theme *Theme) *httptest.ResponseRecorder {
				p, signup, signin, recovery, reset, _, _ := testPages(t)
				signup.status, signin.status, recovery.status, reset.status = tc.actionStatus, tc.actionStatus, tc.actionStatus, tc.actionStatus
				cfg := p.cfg
				cfg.Theme = theme
				cfg.VerificationConfirm = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.actionStatus) })
				p, err := New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				if tc.method == "POST" {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Header.Set("Origin", tc.origin)
				}
				w := httptest.NewRecorder()
				p.Handler().ServeHTTP(w, r)
				return w
			}
			legacy := run(nil)
			themed := run(&Theme{BrandName: "Example", LogoURL: "/assets/logo.svg", StylesheetURL: "/assets/theme.css"})
			if legacy.Code != tc.status || themed.Code != tc.status {
				t.Fatalf("status legacy=%d themed=%d want=%d", legacy.Code, themed.Code, tc.status)
			}
			for _, key := range []string{"Content-Type", "Cache-Control", "Referrer-Policy", "X-Content-Type-Options", "Location", "Set-Cookie"} {
				if !reflect.DeepEqual(legacy.Header().Values(key), themed.Header().Values(key)) {
					t.Fatalf("theme changed %s", key)
				}
			}
			if !reflect.DeepEqual(formControls(t, strings.NewReader(legacy.Body.String())), formControls(t, strings.NewReader(themed.Body.String()))) {
				t.Fatal("theme changed form actions or fields")
			}
			if tc.status == 303 {
				return
			} // Redirects retain their original transport response.
			if !strings.Contains(themed.Header().Get("Content-Security-Policy"), "style-src 'self'; font-src 'self'; img-src 'self'") {
				t.Fatal("missing restricted presentation CSP")
			}
			if strings.Contains(themed.Header().Get("Content-Security-Policy"), "script-src") {
				t.Fatal("theme enabled scripts")
			}
			if tc.method == "HEAD" {
				if themed.Body.Len() != 0 {
					t.Fatal("HEAD emitted a body")
				}
				return
			}
			for _, marker := range []string{`class="amos-auth"`, `class="amos-auth-brand"`, `class="amos-auth-main" id="main-content"`, `href="/assets/theme.css"`} {
				if !strings.Contains(themed.Body.String(), marker) {
					t.Fatalf("missing presentation marker %s", marker)
				}
			}
		})
	}
}

func formControls(t *testing.T, input io.Reader) []string {
	t.Helper()
	root, err := nethtml.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && (n.Data == "form" || n.Data == "input" || n.Data == "button") {
			result = append(result, n.Data)
			for _, a := range n.Attr {
				if a.Key != "class" {
					result = append(result, a.Key+"="+a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return result
}
