// Package authpassword renders the default browser password lifecycle. It is
// deliberately an adapter: credential, challenge and session authority stays
// in the injected identity handlers and services.
package authpassword

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/google/uuid"
)

const maxFormBytes = 8 << 10

type Verification interface {
	Preview(context.Context, uuid.UUID, string) (identityemail.Page, error)
}

// Reset is the narrow presentation surface implemented by identity/recovery.
// Preview is read-only; Complete is supplied as a guarded HTTP action.
type Reset interface {
	Preview(context.Context, uuid.UUID, string) (bool, error)
}

type Config struct {
	Signup, Signin, RecoveryRequest, RecoveryComplete http.Handler
	// VerificationConfirm is the PublicJSON(Verification) guarded action. Its
	// successful completion response must be 204; every other 2xx is ambiguous.
	VerificationConfirm http.Handler
	Verification        Verification
	Reset               Reset
	AllowsOrigin        func(*http.Request) bool
}

type Pages struct{ cfg Config }

func New(cfg Config) (*Pages, error) {
	if cfg.Signup == nil || cfg.Signin == nil || cfg.RecoveryRequest == nil || cfg.RecoveryComplete == nil || cfg.VerificationConfirm == nil || cfg.Verification == nil || cfg.Reset == nil || cfg.AllowsOrigin == nil {
		return nil, errors.New("auth password pages require configured identity actions")
	}
	return &Pages{cfg: cfg}, nil
}

func (p *Pages) Handler() http.Handler { return http.HandlerFunc(p.serve) }

func (p *Pages) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/signup":
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			p.page(w, r, http.StatusOK, model{Title: "Create account", Heading: "Create your account", Action: "/signup", Submit: "Create account", Email: true, Password: true, EmailAuto: "email", PasswordAuto: "new-password", Message: safeMessage(r.URL.Query().Get("message"))})
			return
		}
		p.credentialPost(w, r, p.cfg.Signup, "signup")
	case "/signin", "/auth":
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next := safeReturn(r.URL.Query().Get("next"))
			p.page(w, r, http.StatusOK, model{Title: "Sign in", Heading: "Sign in", Action: "/auth", Submit: "Sign in", Email: true, Password: true, EmailAuto: "username", PasswordAuto: "current-password", Next: next, Message: safeMessage(r.URL.Query().Get("message"))})
			return
		}
		p.credentialPost(w, r, p.cfg.Signin, "signin")
	case "/forgot-password":
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Reset your password", Action: "/forgot-password", Submit: "Send reset instructions", Email: true, EmailAuto: "email", Message: safeMessage(r.URL.Query().Get("message"))})
			return
		}
		p.credentialPost(w, r, p.cfg.RecoveryRequest, "recovery")
	case "/verify-email":
		p.verify(w, r)
	case "/reset-password":
		p.reset(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *Pages) credentialPost(w http.ResponseWriter, r *http.Request, action http.Handler, kind string) {
	if r.Method != http.MethodPost {
		method(w, "GET, HEAD, POST")
		return
	}
	if !p.cfg.AllowsOrigin(r) {
		p.page(w, r, http.StatusForbidden, model{Title: "Request rejected", Heading: "Request could not be accepted", Message: "Return to the page and try again."})
		return
	}
	fields, ok := readForm(w, r, allowed(kind)...)
	if !ok {
		p.page(w, r, http.StatusBadRequest, model{Title: "Check your form", Heading: "Check your form", Message: "Enter the requested information and try again."})
		return
	}
	if kind == "signin" || kind == "signup" {
		if strings.TrimSpace(fields["email"]) == "" || fields["password"] == "" {
			p.page(w, r, http.StatusOK, model{Title: "Check your form", Heading: "Check your form", Action: formAction(kind), Submit: submitLabel(kind), Email: true, Password: true, EmailAuto: emailAuto(kind), PasswordAuto: passwordAuto(kind), Message: "Enter your email and password."})
			return
		}
	}
	payload := map[string]string{"email": fields["email"]}
	if kind == "signin" || kind == "signup" {
		payload["password"] = fields["password"]
	}
	backend := r.Clone(r.Context())
	backend.Body = io.NopCloser(bytes.NewReader(marshal(payload)))
	backend.ContentLength = int64(len(marshal(payload)))
	backend.Header = r.Header.Clone()
	backend.Header.Set("Content-Type", "application/json")
	recorder := newCapture()
	action.ServeHTTP(recorder, backend)
	copySafeActionHeaders(w.Header(), recorder.header)
	status := recorder.statusCode()
	switch kind {
	case "signin":
		if status == http.StatusOK {
			copySetCookies(w.Header(), recorder.header)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "strict-origin")
			http.Redirect(w, r, safeReturn(fields["next"]), http.StatusSeeOther)
			return
		}
		if status >= 500 {
			p.page(w, r, http.StatusServiceUnavailable, model{Title: "Sign in unavailable", Heading: "Sign in is temporarily unavailable", Message: "Please try again shortly."})
			return
		}
		if status == http.StatusTooManyRequests || status == http.StatusForbidden {
			p.page(w, r, status, model{Title: "Sign in", Heading: "Request could not be accepted", Message: messageFor(status)})
			return
		}
		p.page(w, r, http.StatusOK, model{Title: "Sign in", Heading: "Sign in", Action: "/auth", Submit: "Sign in", Email: true, Password: true, EmailAuto: "username", PasswordAuto: "current-password", Next: safeReturn(fields["next"]), Message: "Email or password was not accepted."})
	case "signup":
		if status == http.StatusAccepted {
			p.page(w, r, http.StatusAccepted, model{Title: "Check your email", Heading: "Check your email", Message: "If registration can be completed, instructions will be sent to the address provided.", Link: "/signin", LinkText: "Return to sign in"})
			return
		}
		if status == http.StatusTooManyRequests || status == http.StatusForbidden {
			p.page(w, r, status, model{Title: "Create account", Heading: "Request could not be accepted", Message: messageFor(status)})
			return
		}
		p.page(w, r, publicFailure(status), model{Title: "Create account", Heading: "Create account", Action: "/signup", Submit: "Create account", Email: true, Password: true, EmailAuto: "email", PasswordAuto: "new-password", Message: messageFor(status)})
	case "recovery":
		if status == http.StatusAccepted || status == http.StatusNoContent {
			p.page(w, r, http.StatusOK, model{Title: "Request received", Heading: "Request received", Message: "If an eligible account matches, reset instructions will be sent.", Link: "/signin", LinkText: "Return to sign in"})
			return
		}
		if status == http.StatusTooManyRequests || status == http.StatusForbidden {
			p.page(w, r, status, model{Title: "Reset password", Heading: "Request could not be accepted", Message: messageFor(status)})
			return
		}
		p.page(w, r, publicFailure(status), model{Title: "Reset password", Heading: "Reset your password", Action: "/forgot-password", Submit: "Send reset instructions", Email: true, EmailAuto: "email", Message: messageFor(status)})
	}
}

func (p *Pages) verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		method(w, "GET, HEAD, POST")
		return
	}
	if r.Method == http.MethodPost {
		if !p.cfg.AllowsOrigin(r) {
			p.page(w, r, http.StatusForbidden, model{Title: "Verify email", Heading: "Request rejected", Message: "Return to the verification page and try again."})
			return
		}
		f, ok := readForm(w, r, "challenge", "token")
		if !ok {
			p.page(w, r, http.StatusBadRequest, model{Title: "Verify email", Heading: "Verification request invalid", Message: "Request a new verification email."})
			return
		}
		challenge, err := uuid.Parse(f["challenge"])
		token := f["token"]
		if err != nil || len(token) != 43 {
			p.page(w, r, http.StatusBadRequest, model{Title: "Verify email", Heading: "Verification request invalid", Message: "Request a new verification email."})
			return
		}
		body := marshal(map[string]string{"challenge_id": challenge.String(), "token": token})
		backend := r.Clone(r.Context())
		backend.Body = io.NopCloser(bytes.NewReader(body))
		backend.ContentLength = int64(len(body))
		backend.Header = r.Header.Clone()
		backend.Header.Set("Content-Type", "application/json")
		capture := newCapture()
		p.cfg.VerificationConfirm.ServeHTTP(capture, backend)
		copySafeActionHeaders(w.Header(), capture.header)
		status := capture.statusCode()
		if status == http.StatusTooManyRequests {
			p.page(w, r, status, model{Title: "Verify email", Heading: "Please try again later", Message: messageFor(status)})
			return
		}
		if status >= 500 {
			p.page(w, r, http.StatusServiceUnavailable, model{Title: "Verify email unavailable", Heading: "Email verification is temporarily unavailable", Message: "Please try again shortly."})
			return
		}
		if status != http.StatusNoContent {
			p.page(w, r, http.StatusOK, model{Title: "Verify email", Heading: "Verification link unavailable", Message: "Request a new verification email or return to sign in.", Link: "/signin", LinkText: "Return to sign in"})
			return
		}
		p.page(w, r, http.StatusOK, model{Title: "Email verified", Heading: "Email verified", Message: "Your email is verified. Sign in to continue.", Link: "/signin", LinkText: "Sign in"})
		return
	}
	challenge, token, ok := challengeQuery(r, "challenge")
	if !ok {
		p.page(w, r, http.StatusOK, model{Title: "Verify email", Heading: "Verification link unavailable", Message: "Request a new verification email or return to sign in.", Link: "/signin", LinkText: "Return to sign in"})
		return
	}
	page, err := p.cfg.Verification.Preview(r.Context(), challenge, token)
	if errors.Is(err, identityemail.ErrUnavailable) {
		p.page(w, r, http.StatusServiceUnavailable, model{Title: "Verify email unavailable", Heading: "Email verification is temporarily unavailable", Message: "Please try again shortly."})
		return
	}
	available := err == nil && page.Available
	if !available {
		p.page(w, r, http.StatusOK, model{Title: "Verify email", Heading: "Verification link unavailable", Message: "Request a new verification email or return to sign in.", Link: "/signin", LinkText: "Return to sign in"})
		return
	}
	p.page(w, r, http.StatusOK, model{Title: "Verify email", Heading: "Confirm your email address", Message: "Continue only if you requested this message.", VerifyAction: "/verify-email", Verify: true, Challenge: challenge.String(), Token: token, Submit: "Confirm email"})
}

func (p *Pages) reset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		method(w, "GET, HEAD, POST")
		return
	}
	if r.Method == http.MethodPost {
		if !p.cfg.AllowsOrigin(r) {
			p.page(w, r, http.StatusForbidden, model{Title: "Reset password", Heading: "Request rejected", Message: "Return to the reset page and try again."})
			return
		}
		f, ok := readForm(w, r, "challenge_id", "token", "password")
		if !ok {
			p.page(w, r, http.StatusBadRequest, model{Title: "Reset password", Heading: "Check your form", Message: "Enter a new password and try again."})
			return
		}
		challenge, err := uuid.Parse(f["challenge_id"])
		valid := err == nil && len(f["token"]) == 43 && len(f["password"]) > 0
		if !valid {
			p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Reset link unavailable", Message: "Request a new reset email.", Link: "/forgot-password", LinkText: "Request a new link"})
			return
		}
		body := marshal(map[string]string{"challenge_id": challenge.String(), "token": f["token"], "password": f["password"]})
		backend := r.Clone(r.Context())
		backend.Body = io.NopCloser(bytes.NewReader(body))
		backend.ContentLength = int64(len(body))
		backend.Header = r.Header.Clone()
		backend.Header.Set("Content-Type", "application/json")
		capture := newCapture()
		p.cfg.RecoveryComplete.ServeHTTP(capture, backend)
		copySafeActionHeaders(w.Header(), capture.header)
		status := capture.statusCode()
		if status == http.StatusTooManyRequests || status == http.StatusForbidden {
			p.page(w, r, status, model{Title: "Reset password", Heading: "Request could not be accepted", Message: messageFor(status)})
			return
		}
		if status == http.StatusBadRequest {
			p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Choose a new password", Message: "Choose a password that meets the password requirements and try again.", Action: "/reset-password", Submit: "Change password", Password: true, PasswordAuto: "new-password", Challenge: challenge.String(), Token: f["token"], ResetForm: true})
			return
		}
		if status == http.StatusNoContent {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "strict-origin")
			http.Redirect(w, r, "/signin?message=password-reset", http.StatusSeeOther)
			return
		}
		if status >= 500 {
			p.page(w, r, http.StatusServiceUnavailable, model{Title: "Reset unavailable", Heading: "Password reset is temporarily unavailable", Message: "Please try again shortly."})
			return
		}
		p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Reset link unavailable", Message: "This reset link is invalid, expired or already used. Request a new reset email.", Link: "/forgot-password", LinkText: "Request a new link"})
		return
	}
	challenge, token, ok := challengeQuery(r, "challenge")
	if !ok {
		p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Reset link unavailable", Message: "Request a new reset email.", Link: "/forgot-password", LinkText: "Request a new link"})
		return
	}
	available, err := p.cfg.Reset.Preview(r.Context(), challenge, token)
	if err != nil {
		p.page(w, r, http.StatusServiceUnavailable, model{Title: "Reset unavailable", Heading: "Password reset is temporarily unavailable", Message: "Please try again shortly."})
		return
	}
	if !available {
		p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Reset link unavailable", Message: "Request a new reset email.", Link: "/forgot-password", LinkText: "Request a new link"})
		return
	}
	p.page(w, r, http.StatusOK, model{Title: "Reset password", Heading: "Choose a new password", Message: "This link is single use. Continue only if you requested it.", Action: "/reset-password", Submit: "Change password", Password: true, PasswordAuto: "new-password", Challenge: challenge.String(), Token: token, ResetForm: true})
}

type model struct {
	Title, Heading, Message, Action, Submit, PasswordAuto, EmailAuto, Next, Link, LinkText, VerifyAction, Challenge, Token string
	Email, Password, Verify, ResetForm                                                                                     bool
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="strict-origin"><title>{{.Title}}</title></head><body><main><h1>{{.Heading}}</h1>{{if .Message}}<p role="status">{{.Message}}</p>{{end}}{{if .Action}}<form method="post" action="{{.Action}}">{{if .Email}}<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="{{.EmailAuto}}" required maxlength="320">{{end}}{{if .Password}}<label for="password">{{if .ResetForm}}New password{{else}}Password{{end}}</label><input id="password" name="password" type="password" autocomplete="{{.PasswordAuto}}" required minlength="15" maxlength="128">{{end}}{{if .ResetForm}}<input type="hidden" name="challenge_id" value="{{.Challenge}}"><input type="hidden" name="token" value="{{.Token}}">{{end}}{{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}<button type="submit">{{.Submit}}</button></form>{{end}}{{if .Verify}}<form method="post" action="{{.VerifyAction}}"><input type="hidden" name="challenge" value="{{.Challenge}}"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">{{.Submit}}</button></form>{{end}}{{if .Link}}<p><a href="{{.Link}}">{{.LinkText}}</a></p>{{end}}{{if eq .Title "Verify email"}}<p><a href="/signup">Return to account creation</a></p>{{end}}{{if eq .Title "Sign in"}}<p><a href="/signup">Create account</a> · <a href="/forgot-password">Forgot password?</a></p>{{end}}{{if eq .Title "Create account"}}<p><a href="/signin">Already registered? Sign in</a></p>{{end}}</main></body></html>`))

func (p *Pages) page(w http.ResponseWriter, r *http.Request, status int, m model) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "strict-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = pageTemplate.Execute(w, m)
}
func allowed(kind string) []string {
	switch kind {
	case "signin":
		return []string{"email", "password", "next"}
	case "signup":
		return []string{"email", "password"}
	case "recovery":
		return []string{"email"}
	default:
		return nil
	}
}
func formAction(k string) string {
	if k == "signin" {
		return "/auth"
	}
	return "/signup"
}
func submitLabel(k string) string {
	if k == "signin" {
		return "Sign in"
	}
	return "Create account"
}
func emailAuto(k string) string {
	if k == "signin" {
		return "username"
	}
	return "email"
}
func passwordAuto(k string) string {
	if k == "signin" {
		return "current-password"
	}
	return "new-password"
}
func publicFailure(status int) int {
	if status < 400 || status > 599 {
		return http.StatusServiceUnavailable
	}
	if status >= 500 {
		return http.StatusServiceUnavailable
	}
	if status == http.StatusTooManyRequests || status == http.StatusForbidden {
		return status
	}
	return http.StatusOK
}
func messageFor(status int) string {
	if status >= 500 {
		return "This service is temporarily unavailable. Please try again shortly."
	}
	if status == http.StatusTooManyRequests {
		return "Too many attempts. Please wait and try again."
	}
	return "The request could not be completed. Check the information and try again."
}
func safeReturn(raw string) string {
	if raw == "" {
		return "/todos"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		return "/todos"
	}
	if u.Path != "/todos" && u.Path != "/account" && u.Path != "/workspaces" {
		return "/todos"
	}
	return u.Path
}
func readForm(w http.ResponseWriter, r *http.Request, names ...string) (map[string]string, bool) {
	if r.Body == nil || r.ContentLength > maxFormBytes || r.URL.RawQuery != "" || len(r.Header.Values("Content-Type")) != 1 {
		return nil, false
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if ct != "application/x-www-form-urlencoded" {
		return nil, false
	}
	limited := http.MaxBytesReader(w, r.Body, maxFormBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	if len(values) > len(allowed) {
		return nil, false
	}
	out := map[string]string{}
	for k, v := range values {
		if !allowed[k] || len(v) != 1 || !utf8.ValidString(v[0]) || hasControl(v[0]) {
			return nil, false
		}
		out[k] = v[0]
	}
	return out, true
}
func challengeQuery(r *http.Request, idKey string) (uuid.UUID, string, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 2 || len(values[idKey]) != 1 || len(values["token"]) != 1 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(values[idKey][0])
	token := values["token"][0]
	if err != nil || id.String() != values[idKey][0] || len(token) != 43 || hasControl(token) {
		return uuid.Nil, "", false
	}
	return id, token, true
}
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
func safeMessage(s string) string {
	if s == "password-reset" {
		return "Your password was changed. Sign in with your new password."
	}
	return ""
}
func marshal(v any) []byte { b, _ := json.Marshal(v); return b }
func method(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
}

type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCapture() *capture             { return &capture{header: make(http.Header)} }
func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(s int) {
	if c.status == 0 {
		c.status = s
	}
}
func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	const responseLimit = 16 << 10
	if c.body.Len() < responseLimit {
		remaining := responseLimit - c.body.Len()
		if len(b) > remaining {
			b = b[:remaining]
		}
		return c.body.Write(b)
	}
	return len(b), nil
}
func (c *capture) statusCode() int {
	if c.status == 0 {
		return 200
	}
	return c.status
}
func copySafeActionHeaders(dst, src http.Header) {
	for _, key := range []string{"Retry-After", "X-Request-ID"} {
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
}

func copySetCookies(dst, src http.Header) {
	for _, value := range src.Values("Set-Cookie") {
		dst.Add("Set-Cookie", value)
	}
}
