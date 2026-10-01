package authpassword

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/google/uuid"
)

type action struct {
	status  int
	calls   int
	payload map[string]string
}

func (a *action) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.calls++
	defer func() { _ = r.Body.Close() }()
	_ = json.NewDecoder(r.Body).Decode(&a.payload)
	w.Header().Add("Set-Cookie", "session=opaque; Path=/; HttpOnly; SameSite=Lax")
	w.WriteHeader(a.status)
}

type verify struct {
	preview            bool
	previews, confirms int
}

func (v *verify) Preview(context.Context, uuid.UUID, string) (identityemail.Page, error) {
	v.previews++
	return identityemail.Page{Available: v.preview}, nil
}

type reset struct {
	available bool
	previews  int
}

func (v *reset) Preview(context.Context, uuid.UUID, string) (bool, error) {
	v.previews++
	return v.available, nil
}

func testPages(t *testing.T) (*Pages, *action, *action, *action, *action, *verify, *reset) {
	t.Helper()
	signup := &action{status: 202}
	signin := &action{status: 200}
	request := &action{status: 202}
	complete := &action{status: 204}
	v := &verify{preview: true}
	rs := &reset{available: true}
	confirm := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Origin") != "https://app.example" || json.NewDecoder(r.Body).Decode(&payload) != nil || payload["challenge_id"] == "" || payload["token"] == "" {
			http.Error(w, "invalid confirmation adapter", http.StatusBadRequest)
			return
		}
		v.confirms++
		w.WriteHeader(http.StatusNoContent)
	})
	p, err := New(Config{Signup: signup, Signin: signin, RecoveryRequest: request, RecoveryComplete: complete, VerificationConfirm: confirm, Verification: v, Reset: rs, AllowsOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://app.example" }})
	if err != nil {
		t.Fatal(err)
	}
	return p, signup, signin, request, complete, v, rs
}

func TestT6_3_SignupAndSigninActionsStayBehindIdentityHandlers(t *testing.T) {
	p, signup, signin, _, _, _, _ := testPages(t)
	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://app.example")
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, r)
		return w
	}
	w := post("/signup", url.Values{"email": {"a@example.test"}, "password": {"not-a-real-secret"}})
	if w.Code != 202 || signup.calls != 1 || signup.payload["password"] != "not-a-real-secret" {
		t.Fatalf("signup delegated incorrectly: status %d calls %d", w.Code, signup.calls)
	}
	w = post("/auth", url.Values{"email": {"a@example.test"}, "password": {"wrong"}, "next": {"https://evil.example"}})
	if w.Code != 303 || w.Header().Get("Location") != "/todos" || w.Header().Get("Cache-Control") != "no-store" || signin.calls != 1 {
		t.Fatalf("unsafe sign-in response: %d %q", w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "session=opaque") {
		t.Fatal("identity session cookie was not preserved")
	}
	w = post("/auth", url.Values{"email": {"a@example.test", "b@example.test"}, "password": {"x"}})
	if w.Code != 400 || signin.calls != 1 {
		t.Fatalf("duplicate field reached identity action: status %d calls %d", w.Code, signin.calls)
	}
	r := httptest.NewRequest("POST", "/auth", strings.NewReader(url.Values{"email": {"a@example.test"}, "password": {"x"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://foreign.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 403 || signin.calls != 1 {
		t.Fatalf("cross-origin form reached identity action: status %d calls %d", w.Code, signin.calls)
	}
	signin.status = http.StatusUnauthorized
	w = post("/auth", url.Values{"email": {"a@example.test"}, "password": {"wrong"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Email or password was not accepted") || strings.Contains(w.Body.String(), "wrong") {
		t.Fatal("wrong password response was not generic and actionable")
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("failed sign-in propagated a session cookie")
	}
}

func TestT6_3_ScannerPreviewDoesNotConfirmAndExplicitPostConfirms(t *testing.T) {
	p, _, _, _, _, v, _ := testPages(t)
	id := uuid.New()
	token := strings.Repeat("a", 43)
	path := "/verify-email?challenge=" + id.String() + "&token=" + token
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, r)
		if w.Code != 200 || v.confirms != 0 || w.Header().Get("Referrer-Policy") != "strict-origin" {
			t.Fatalf("scanner preview changed challenge or missed privacy headers: %s %d", method, w.Code)
		}
	}
	duplicate := httptest.NewRequest("GET", path+"&token="+token, nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, duplicate)
	if v.previews != 2 || v.confirms != 0 || !strings.Contains(w.Body.String(), "link unavailable") {
		t.Fatal("duplicate challenge query reached the preview authority")
	}
	r := httptest.NewRequest("POST", "/verify-email", strings.NewReader(url.Values{"challenge": {id.String()}, "token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 200 || v.confirms != 1 || !strings.Contains(w.Body.String(), "Sign in") {
		t.Fatalf("explicit confirmation failed: status %d", w.Code)
	}
	p.cfg.VerificationConfirm = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r = httptest.NewRequest("POST", "/verify-email", strings.NewReader(url.Values{"challenge": {id.String()}, "token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if v.confirms != 1 || !strings.Contains(w.Body.String(), "Verification link unavailable") {
		t.Fatal("ambiguous confirmation response was mistaken for successful verification")
	}
}

func TestT6_3_ResetReplayIsActionableAndDoesNotIssueSession(t *testing.T) {
	p, _, _, _, complete, _, rs := testPages(t)
	id := uuid.New()
	token := strings.Repeat("b", 43)
	path := "/reset-password?challenge=" + id.String() + "&token=" + token
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 200 || rs.previews != 1 || strings.Contains(w.Body.String(), "session=") {
		t.Fatal("reset preview unexpectedly consumed or authenticated")
	}
	complete.status = http.StatusBadRequest
	r = httptest.NewRequest("POST", "/reset-password", strings.NewReader(url.Values{"challenge_id": {id.String()}, "token": {token}, "password": {"weak"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "password requirements") || strings.Contains(w.Body.String(), "weak") || strings.Contains(w.Body.String(), `value="weak"`) {
		t.Fatal("password validation was not actionable or echoed the password")
	}
	complete.status = 204
	r = httptest.NewRequest("POST", "/reset-password", strings.NewReader(url.Values{"challenge_id": {id.String()}, "token": {token}, "password": {"new-password"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || complete.calls != 2 || w.Header().Get("Location") != "/signin?message=password-reset" || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("successful reset did not require a fresh sign in")
	}
	complete.status = http.StatusOK
	r = httptest.NewRequest("POST", "/reset-password", strings.NewReader(url.Values{"challenge_id": {id.String()}, "token": {token}, "password": {"another-password"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "/signin?message=password-reset") || strings.Contains(w.Body.String(), "Password updated") || !strings.Contains(w.Body.String(), "invalid, expired or already used") {
		t.Fatal("ambiguous 200 response was presented as a completed reset")
	}
	complete.status = 409
	r = httptest.NewRequest("POST", "/reset-password", strings.NewReader(url.Values{"challenge_id": {id.String()}, "token": {token}, "password": {"another-password"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "invalid, expired or already used") || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("replayed reset did not render a safe actionable denial")
	}
}

func TestT6_3_GenericDependencyErrorsAndBoundedForm(t *testing.T) {
	p, _, signin, _, _, _, _ := testPages(t)
	if got := safeReturn("/todos?next=https%3A%2F%2Fevil.example"); got != "/todos" {
		t.Fatalf("redirect retained an unvalidated query: %q", got)
	}
	signin.status = 503
	r := httptest.NewRequest("POST", "/auth", strings.NewReader(url.Values{"email": {"x@example.test"}, "password": {"x"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "database") || strings.Contains(w.Body.String(), "SQL") {
		t.Fatal("dependency detail escaped")
	}
	r = httptest.NewRequest("POST", "/auth", strings.NewReader(url.Values{"email": {"x@example.test"}, "password": {strings.Repeat("x", maxFormBytes)}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://app.example")
	w = httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("oversized form accepted: %d", w.Code)
	}
}
