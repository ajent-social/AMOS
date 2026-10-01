package protection

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
)

const MaxAuthBody = 4096
const MaxMutationBody = 16384

// Guard is composed with authoritative session middleware. Callback routes must
// use their separately bound state proof and cannot enter these wrappers.
type Guard struct {
	Limiter  *Limiter
	Sessions *session.Service
}

func (g Guard) valid() bool { return g.Limiter != nil && g.Sessions != nil }
func fail(w http.ResponseWriter, status int, code string) {
	id, err := uuid.NewV7()
	if err != nil {
		http.Error(w, "Request unavailable.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("X-Request-ID", id.String())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": "Authentication request unavailable.", "id": id.String()})
}
func boundedBody(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(w, r.Body, max))
}
func (g Guard) admission(w http.ResponseWriter, r *http.Request, op Operation, account string) bool {
	ip, err := PeerIP(r)
	if err != nil {
		fail(w, 400, "auth.invalid_request")
		return false
	}
	result, err := g.Limiter.Allow(r.Context(), op, ip, account)
	if err != nil {
		fail(w, 503, "dependency.unavailable")
		return false
	}
	if !result.Allowed {
		seconds := int((result.RetryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		fail(w, 429, "auth.rate_limited")
		return false
	}
	return true
}

// PublicJSON admits signup/signin requests before any credential work. It does
// not resolve account existence and never accepts identity from request data.
func (g Guard) PublicJSON(op Operation, next http.Handler) (http.Handler, error) {
	if !g.valid() || next == nil || (op != Signup && op != Signin && op != Recovery && op != Verification) {
		return nil, ErrConfiguration
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fail(w, 405, "method.not_allowed")
			return
		}
		if !g.Sessions.AllowsOrigin(r) {
			fail(w, 403, "auth.forbidden")
			return
		}
		ct := r.Header.Values("Content-Type")
		if len(ct) != 1 {
			fail(w, 400, "auth.invalid_request")
			return
		}
		media, _, err := mime.ParseMediaType(ct[0])
		if err != nil || media != "application/json" {
			fail(w, 400, "auth.invalid_request")
			return
		}
		body, err := boundedBody(w, r, MaxAuthBody)
		if err != nil {
			fail(w, 413, "request.too_large")
			return
		}
		var key struct {
			Email string `json:"email"`
		}
		if json.Unmarshal(body, &key) != nil || len(key.Email) > 320 || !utf8.ValidString(key.Email) || strings.ContainsAny(key.Email, "\x00\r\n") {
			key.Email = "invalid-input"
		}
		if !g.admission(w, r, op, key.Email) {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	}), nil
}

// CookieMutation requires a current principal already attached by trusted
// session middleware, plus origin and exact body/header CSRF evidence.
func (g Guard) CookieMutation(op Operation, next http.Handler) (http.Handler, error) {
	if !g.valid() || next == nil || !validOperation(op) {
		return nil, ErrConfiguration
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			fail(w, 405, "method.not_allowed")
			return
		}
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok {
			fail(w, 401, "auth.unauthenticated")
			return
		}
		if principal.InstallationID().String() != g.Limiter.cfg.InstallationID.String() || principal.ApplicationID().String() != g.Limiter.cfg.ApplicationID.String() || principal.EnvironmentID().String() != g.Limiter.cfg.EnvironmentID.String() {
			fail(w, 403, "auth.forbidden")
			return
		}
		if !g.Sessions.AllowsOrigin(r) {
			fail(w, 403, "auth.forbidden")
			return
		}
		expected, ok := g.Sessions.CSRFToken(r)
		if !ok {
			fail(w, 403, "auth.forbidden")
			return
		}
		body, err := boundedBody(w, r, MaxMutationBody)
		if err != nil {
			fail(w, 413, "request.too_large")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		values := r.Header.Values("X-CSRF-Token")
		var supplied string
		if len(values) == 1 {
			supplied = values[0]
		} else if len(values) == 0 {
			types := r.Header.Values("Content-Type")
			if len(types) == 1 {
				media, _, e := mime.ParseMediaType(types[0])
				if e == nil && media == "application/x-www-form-urlencoded" {
					form, e := url.ParseQuery(string(body))
					if e == nil && len(form["_csrf"]) == 1 {
						supplied = form["_csrf"][0]
					}
				}
			}
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) != 1 {
			fail(w, 403, "auth.forbidden")
			return
		}
		person := principal.Actor().PersonID()
		if person == uuid.Nil {
			fail(w, 403, "auth.forbidden")
			return
		}
		if !g.admission(w, r, op, person.String()) {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	}), nil
}

// RelativeRedirect rejects ambiguous or external targets before Location is set.
func RelativeRedirect(target string) (string, error) {
	if len(target) == 0 || len(target) > 2048 || !utf8.ValidString(target) || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.ContainsAny(target, "\\\x00\r\n") {
		return "", ErrConfiguration
	}
	for _, c := range target {
		if c < 32 || c == 127 {
			return "", ErrConfiguration
		}
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" {
		return "", ErrConfiguration
	}
	if strings.Contains(u.Path, "//") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "%") {
		return "", ErrConfiguration
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return "", ErrConfiguration
		}
	}
	for _, c := range u.Path {
		if c < 32 || c == 127 {
			return "", ErrConfiguration
		}
	}
	if u.RawPath != "" {
		return "", ErrConfiguration
	}
	return u.String(), nil
}
