// Package session implements durable opaque browser sessions. It never
// derives workspace authority from cookie contents or request selectors.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	ErrUnauthenticated      = errors.New("unauthenticated")
	ErrUnavailable          = errors.New("authentication unavailable")
	ErrInvalidConfiguration = errors.New("invalid session configuration")
)

const (
	productionCookie  = "__Host-amos_session"
	developmentCookie = "amos_dev_session"
	maxAge            = 12 * time.Hour
)

type Config struct {
	InstallationID      uuid.UUID
	ApplicationID       uuid.UUID
	EnvironmentID       uuid.UUID
	AllowedOrigins      []string
	DevelopmentLoopback bool
	CookieSecure        bool
	// PersistAssurance requires the optional SessionAssurance migration.
	PersistAssurance bool
}

// TxRunner is the transaction capability required by a session service.
// The caller owns its lifetime and binds it to the intended database.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type Service struct {
	root       *aw.Root
	db         TxRunner
	cfg        Config
	cookieName string
}

func New(db *storage.DB, cfg Config) (*Service, error) {
	return NewWithTxRunner(db, cfg)
}

// NewWithTxRunner constructs a service without probing or taking ownership of db.
func NewWithTxRunner(db TxRunner, cfg Config) (*Service, error) {
	if nilTxRunner(db) {
		return nil, ErrInvalidConfiguration
	}
	if err := aw.SelectLegacy(); err != nil {
		return nil, ErrInvalidConfiguration
	}
	s, err := configured(cfg)
	if err != nil {
		return nil, err
	}
	s.db = db
	return s, nil
}

func configured(cfg Config) (*Service, error) {
	cfg.AllowedOrigins = append([]string(nil), cfg.AllowedOrigins...)
	if !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) || len(cfg.AllowedOrigins) == 0 {
		return nil, ErrInvalidConfiguration
	}
	if cfg.DevelopmentLoopback {
		if cfg.CookieSecure {
			return nil, ErrInvalidConfiguration
		}
		for _, origin := range cfg.AllowedOrigins {
			if !isLoopbackOrigin(origin) {
				return nil, ErrInvalidConfiguration
			}
		}
		return &Service{cfg: cfg, cookieName: developmentCookie}, nil
	}
	if !cfg.CookieSecure {
		return nil, ErrInvalidConfiguration
	}
	for _, origin := range cfg.AllowedOrigins {
		if !validHTTPSOrigin(origin) {
			return nil, ErrInvalidConfiguration
		}
	}
	return &Service{cfg: cfg, cookieName: productionCookie}, nil
}

func nilTxRunner(db TxRunner) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

type Issued struct {
	Cookie           *http.Cookie
	CSRFToken        string
	AssuranceExpires time.Time
}

// Issue rotates any known prior browser session and creates a fresh one in a
// single transaction. Proof construction belongs to credential adapters.
func (s *Service) Issue(ctx context.Context, proof authproof.VerifiedCredential, priorToken string) (Issued, error) {
	if s == nil || s.db == nil || ctx == nil {
		return Issued{}, ErrUnavailable
	}
	var issued Issued
	err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error { var err error; issued, err = s.issueTx(ctx, tx, proof, priorToken); return err })
	if err != nil {
		return Issued{}, ErrUnavailable
	}
	return issued, nil
}

// IssueForRequestTx stages browser-session rotation in the caller's transaction.
// The caller must commit successfully before exposing either the returned cookie
// or CSRF token. A rollback also rolls back challenge consumption and rotation.
// Credential adapters must establish current identity and policy before calling.
func (s *Service) IssueForRequestTx(ctx context.Context, tx *sql.Tx, proof authproof.VerifiedCredential, r *http.Request) (Issued, error) {
	if s == nil || r == nil {
		return Issued{}, ErrUnavailable
	}
	var prior string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == s.cookieName {
			count++
			prior = cookie.Value
		}
	}
	if count > 1 {
		return Issued{}, ErrUnauthenticated
	}
	return s.issueTx(ctx, tx, proof, prior)
}

func (s *Service) issueTx(ctx context.Context, tx *sql.Tx, proof authproof.VerifiedCredential, priorToken string) (Issued, error) {
	if s != nil && s.root != nil {
		return Issued{}, ErrUnavailable
	}
	if err := aw.SelectLegacy(); err != nil {
		return Issued{}, ErrUnavailable
	}
	if ctx == nil || tx == nil || s == nil || proof.PersonID() == uuid.Nil || proof.InstallationID() != s.cfg.InstallationID || proof.ApplicationID() != s.cfg.ApplicationID || proof.EnvironmentID() != s.cfg.EnvironmentID {
		return Issued{}, ErrUnauthenticated
	}
	now := time.Now().UTC()
	if proof.AuthenticatedAt().After(now) || proof.AssuranceExpires().Before(now) {
		return Issued{}, ErrUnauthenticated
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Issued{}, ErrUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	id, err := store.NewID()
	if err != nil {
		return Issued{}, ErrUnavailable
	}
	expires := proof.AuthenticatedAt().Add(maxAge)
	if proof.Assurance() == "aal1" && proof.AssuranceExpires().Before(expires) {
		expires = proof.AssuranceExpires()
	}
	if proof.Assurance() != "aal1" && (!s.cfg.PersistAssurance || proof.AssuranceExpires().After(proof.AuthenticatedAt().Add(15*time.Minute))) {
		return Issued{}, ErrUnauthenticated
	}

	err = func() error {
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		if priorToken != "" {
			if old, ok := tokenDigest(priorToken); ok {
				if err := st.RevokeSessionScoped(ctx, old, store.SessionScope{InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID}); err != nil && !errors.Is(err, store.ErrSessionUnavailable) {
					return err
				}
			}
		}
		err = st.CreateSession(ctx, store.Session{ID: id, PersonID: proof.PersonID(), InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID, TokenDigest: digest[:], SecurityEpoch: proof.SecurityEpoch(), AuthenticationMethod: proof.Method(), AuthenticatedAt: proof.AuthenticatedAt(), ExpiresAt: expires})
		if err != nil {
			return err
		}
		if proof.Assurance() != "aal1" {
			return st.SetSessionAssurance(ctx, id, proof.Assurance(), proof.AssuranceExpires())
		}
		return nil
	}()
	if err != nil {
		return Issued{}, ErrUnavailable
	}
	return Issued{Cookie: s.cookie(token, expires), CSRFToken: csrf(raw), AssuranceExpires: proof.AssuranceExpires()}, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s != nil && s.root != nil {
			s.writerMiddleware(next, w, r)
			return
		}
		if s == nil || s.db == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		cookie, err := singleCookie(r, s.cookieName)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
			return
		}
		digest, ok := tokenDigest(cookie.Value)
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
			return
		}
		var active store.AuthenticatedSession
		level := "aal1"
		var assuranceExpiry time.Time
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			st, e := store.New(tx)
			if e != nil {
				return e
			}
			active, e = st.FindActiveSession(ctx, digest, store.SessionScope{InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID})
			if e == nil && s.cfg.PersistAssurance {
				level, assuranceExpiry, e = st.ActiveSessionAssurance(ctx, active.ID)
			}
			return e
		})
		if err != nil {
			if errors.Is(err, store.ErrSessionUnavailable) {
				writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
			} else {
				writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			}
			return
		}
		if !s.cfg.PersistAssurance {
			assuranceExpiry = active.AuthenticatedAt.Add(maxAge)
		}
		proof, err := authproof.NewVerifiedCredential(active.PersonID, active.InstallationID, active.ApplicationID, active.EnvironmentID, active.SecurityEpoch, active.AuthenticationMethod, active.AuthenticatedAt, level, assuranceExpiry)
		if err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		if unsafeMethod(r.Method) {
			if !s.AllowsOrigin(r) {
				writeError(w, r, http.StatusForbidden, "request.origin_denied")
				return
			}
			raw, _ := base64.RawURLEncoding.DecodeString(cookie.Value)
			provided := csrfFromRequest(w, r)
			if len(raw) != 32 || !hmac.Equal([]byte(provided), []byte(csrf(raw))) {
				writeError(w, r, http.StatusForbidden, "request.csrf_denied")
				return
			}
		}
		admittedCtx, stop := context.WithTimeout(r.Context(), 5*time.Second)
		defer stop()
		admittedCtx = identity.ContextWithVerifiedCredential(admittedCtx, proof)
		principal, _ := identity.PrincipalFromContext(admittedCtx)
		admittedCtx = s.admitCurrent(admittedCtx, active.ID, principal, digest)
		next.ServeHTTP(w, r.WithContext(admittedCtx))
	})
}

func (s *Service) SignOut(w http.ResponseWriter, r *http.Request) {
	if s != nil && s.root != nil {
		s.writerSignOut(w, r)
		return
	}
	if s == nil || s.db == nil {
		writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed")
		return
	}
	if !s.AllowsOrigin(r) {
		writeError(w, r, http.StatusForbidden, "request.origin_denied")
		return
	}
	cookie, err := r.Cookie(s.cookieName)
	if err == nil {
		raw, _ := base64.RawURLEncoding.DecodeString(cookie.Value)
		if len(raw) != 32 || !hmac.Equal([]byte(csrfFromRequest(w, r)), []byte(csrf(raw))) {
			writeError(w, r, http.StatusForbidden, "request.csrf_denied")
			return
		}
		digest := sha256.Sum256([]byte(cookie.Value))
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			st, e := store.New(tx)
			if e != nil {
				return e
			}
			e = st.RevokeSession(ctx, digest[:])
			if errors.Is(e, store.ErrSessionUnavailable) {
				return nil
			}
			return e
		})
		if err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
	}
	http.SetCookie(w, s.expiredCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) CSRFToken(r *http.Request) (string, bool) {
	c, e := r.Cookie(s.cookieName)
	if e != nil {
		return "", false
	}
	raw, e := base64.RawURLEncoding.DecodeString(c.Value)
	if e != nil || len(raw) != 32 {
		return "", false
	}
	return csrf(raw), true
}

func (s *Service) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: s.cookieName, Value: value, Path: "/", Secure: s.cfg.CookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())}
}
func (s *Service) expiredCookie() *http.Cookie {
	c := s.cookie("", time.Unix(1, 0))
	c.MaxAge = -1
	return c
}
func tokenDigest(token string) ([]byte, bool) {
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}
func csrf(raw []byte) string {
	m := hmac.New(sha256.New, raw)
	m.Write([]byte("amos-session-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func unsafeMethod(m string) bool { return m != "GET" && m != "HEAD" && m != "OPTIONS" }
func (s *Service) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if referer := r.Referer(); referer != "" {
			parsed, err := url.Parse(referer)
			if err != nil || parsed.User != nil || parsed.Host == "" {
				return false
			}
			origin = parsed.Scheme + "://" + parsed.Host
		}
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validHTTPSOrigin(v string) bool {
	u, err := url.Parse(v)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func isLoopbackOrigin(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code string) {
	id := uuid.NewString()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", id)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"` + code + `","message":"request could not be completed","request_id":"` + id + `"}`))
}

// csrfFromRequest accepts the existing API header or a single body-only token
// from bounded ordinary HTML forms. Query parameters never supply proof.
func csrfFromRequest(w http.ResponseWriter, r *http.Request) string {
	values := r.Header.Values("X-CSRF-Token")
	if len(values) > 0 {
		if len(values) != 1 {
			return ""
		}
		return values[0]
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		return ""
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err = r.ParseForm(); err != nil {
		return ""
	}
	values = r.PostForm["_csrf"]
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

// IssueForRequest rotates the existing browser cookie while establishing a new
// authenticated session. Invalid/missing cookies cannot choose session IDs.
func (s *Service) IssueForRequest(ctx context.Context, proof authproof.VerifiedCredential, r *http.Request) (Issued, error) {
	if s == nil || r == nil {
		return Issued{}, ErrUnavailable
	}
	var prior string
	if cookie, err := r.Cookie(s.cookieName); err == nil {
		prior = cookie.Value
	}
	return s.Issue(ctx, proof, prior)
}

// AllowsOrigin applies the configured browser origin boundary before authentication.
func (s *Service) AllowsOrigin(r *http.Request) bool {
	if s == nil || r == nil || len(r.Header.Values("Origin")) > 1 || len(r.Header.Values("Referer")) > 1 {
		return false
	}
	return s.allowedOrigin(r)
}
