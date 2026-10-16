package authpassword

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReturnPolicyValidationAndCopy(t *testing.T) {
	invalid := []ReturnPolicy{
		{}, {Default: "/a", Allowed: []string{"/b"}}, {Default: "/a", Allowed: []string{"/a", "/a"}},
	}
	for _, path := range []string{"", "https://foreign.example/a", "//foreign.example/a", "/a/../b", "/a/", "/a//b", "/a%2fb", "/a?x=y", "/a#fragment", "/a\\b", "/a\x00", "/é", "/" + strings.Repeat("a", 2048)} {
		invalid = append(invalid, ReturnPolicy{Default: path, Allowed: []string{path}})
	}
	many := ReturnPolicy{Default: "/", Allowed: []string{"/"}}
	for i := 0; i < 32; i++ {
		many.Allowed = append(many.Allowed, "/"+strings.Repeat("a", i+1))
	}
	invalid = append(invalid, many)
	for _, policy := range invalid {
		if !errors.Is(policy.Validate(), ErrInvalidReturnPolicy) {
			t.Fatalf("accepted invalid policy %#v", policy)
		}
		p, _, _, _, _, _, _ := testPages(t)
		cfg := p.cfg
		cfg.ReturnPolicy = &policy
		if _, err := New(cfg); !errors.Is(err, ErrInvalidReturnPolicy) {
			t.Fatal("constructor accepted invalid policy")
		}
	}
	for _, path := range []string{"/", "/consumer/account", "/" + strings.Repeat("a", 2047)} {
		if err := (ReturnPolicy{Default: path, Allowed: []string{path}}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	p, _, _, _, _, _, _ := testPages(t)
	policy := ReturnPolicy{Default: "/consumer/account", Allowed: []string{"/consumer/account", "/consumer/other"}}
	cfg := p.cfg
	cfg.ReturnPolicy = &policy
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	policy.Default = "https://foreign.example"
	policy.Allowed[0] = "/changed"
	policy.Allowed[1] = "/other-change"
	if p.returnPath("") != "/consumer/account" || p.returnPath("/consumer/other") != "/consumer/other" {
		t.Fatal("constructor retained caller navigation memory")
	}
}

func TestReturnPolicyRejectsUnsafeSelection(t *testing.T) {
	for _, next := range []string{"", "/unknown", "https://foreign.example/a", "//foreign.example/a", "/consumer/account/extra", "/consumer/account?private=value", "/consumer/account#x", "/consumer/%61ccount", "/consumer/../consumer/account", "/consumer/account\\x", "/consumer/account\r\nLocation: https://foreign.example", "/consumer//account", "/consumer/other"} {
		t.Run(next, func(t *testing.T) {
			base, _, signin, _, _, _, _ := testPages(t)
			cfg := base.cfg
			cfg.ReturnPolicy = &ReturnPolicy{Default: "/consumer/account", Allowed: []string{"/consumer/account", "/consumer/other"}}
			p, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			want := "/consumer/account"
			if next == "/consumer/other" {
				want = next
			}
			w := httptest.NewRecorder()
			p.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/signin?"+url.Values{"next": {next}}.Encode(), nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `name="next" value="`+want+`"`) {
				t.Fatal("GET exposed unsafe return selection")
			}
			post := func(values url.Values, origin string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", "/auth", strings.NewReader(values.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", origin)
				w := httptest.NewRecorder()
				p.Handler().ServeHTTP(w, req)
				return w
			}
			values := url.Values{"email": {"synthetic@example.test"}, "password": {"synthetic-password"}, "next": {next}}
			w = post(values, "https://app.example")
			if strings.ContainsAny(next, "\r\n") {
				if w.Code != 400 || w.Header().Get("Location") != "" || signin.calls != 0 {
					t.Fatal("control input bypassed strict form parser")
				}
				return
			}
			if w.Code != 303 || w.Header().Get("Location") != want || w.Header().Get("Set-Cookie") == "" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unsafe successful return: %d %q", w.Code, w.Header().Get("Location"))
			}
			signin.status = 401
			w = post(values, "https://app.example")
			if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || !strings.Contains(w.Body.String(), `name="next" value="`+want+`"`) {
				t.Fatal("failed sign-in lost safe return or issued cookie")
			}
			values.Del("password")
			w = post(values, "https://app.example")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `name="next" value="`+want+`"`) {
				t.Fatal("missing-field form lost safe return")
			}
			before := signin.calls
			w = post(values, "https://foreign.example")
			if w.Code != 403 || signin.calls != before {
				t.Fatal("return policy bypassed Origin validation")
			}
			values["next"] = []string{"/consumer/account", "/consumer/other"}
			w = post(values, "https://app.example")
			if w.Code != 400 || signin.calls != before {
				t.Fatal("return policy bypassed duplicate-form rejection")
			}
		})
	}
}
