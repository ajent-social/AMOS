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
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/store"
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
}

type Service struct {
	db         *storage.DB
	cfg        Config
	cookieName string
}

func New(db *storage.DB, cfg Config) (*Service, error) {
	if db == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) || len(cfg.AllowedOrigins) == 0 {
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
		return &Service{db: db, cfg: cfg, cookieName: developmentCookie}, nil
	}
	if !cfg.CookieSecure {
		return nil, ErrInvalidConfiguration
	}
	for _, origin := range cfg.AllowedOrigins {
		if !validHTTPSOrigin(origin) {
			return nil, ErrInvalidConfiguration
		}
	}
	return &Service{db: db, cfg: cfg, cookieName: productionCookie}, nil
}

type Issued struct {
	Cookie    *http.Cookie
	CSRFToken string
}

// Issue rotates any known prior browser session and creates a fresh one in a
// single transaction. Proof construction belongs to credential adapters.
func (s *Service) Issue(ctx context.Context, proof authproof.VerifiedCredential, priorToken string) (Issued, error) {
	if s == nil || proof.PersonID() == uuid.Nil || proof.InstallationID() != s.cfg.InstallationID || proof.ApplicationID() != s.cfg.ApplicationID || proof.EnvironmentID() != s.cfg.EnvironmentID {
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
	if proof.AssuranceExpires().Before(expires) {
		// The current persistence schema stores sign-in method/time but not
		// elevated assurance. Bound the durable session to the supplied proof
		// until that schema can preserve a stronger assurance level.
		expires = proof.AssuranceExpires()
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		if priorToken != "" {
			if old, ok := tokenDigest(priorToken); ok {
				if err := st.RevokeSession(ctx, old); err != nil && !errors.Is(err, store.ErrSessionUnavailable) {
					return err
				}
			}
		}
		return st.CreateSession(ctx, store.Session{ID: id, PersonID: proof.PersonID(), InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID, TokenDigest: digest[:], SecurityEpoch: proof.SecurityEpoch(), AuthenticationMethod: proof.Method(), AuthenticatedAt: proof.AuthenticatedAt(), ExpiresAt: expires})
	})
	if err != nil {
		return Issued{}, ErrUnavailable
	}
	return Issued{Cookie: s.cookie(token, expires), CSRFToken: csrf(raw)}, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.db == nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		cookie, err := r.Cookie(s.cookieName)
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
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			st, e := store.New(tx)
			if e != nil {
				return e
			}
			active, e = st.FindActiveSession(ctx, digest, store.SessionScope{InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID})
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
		proof, err := authproof.NewVerifiedCredential(active.PersonID, active.InstallationID, active.ApplicationID, active.EnvironmentID, active.SecurityEpoch, active.AuthenticationMethod, active.AuthenticatedAt, "aal1", active.AuthenticatedAt.Add(maxAge))
		if err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		if unsafeMethod(r.Method) {
			if !s.allowedOrigin(r) {
				writeError(w, r, http.StatusForbidden, "request.origin_denied")
				return
			}
			raw, _ := base64.RawURLEncoding.DecodeString(cookie.Value)
			provided := r.Header.Get("X-CSRF-Token")
			if len(raw) != 32 || !hmac.Equal([]byte(provided), []byte(csrf(raw))) {
				writeError(w, r, http.StatusForbidden, "request.csrf_denied")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(identity.ContextWithVerifiedCredential(r.Context(), proof)))
	})
}

func (s *Service) SignOut(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.db == nil {
		writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed")
		return
	}
	if !s.allowedOrigin(r) {
		writeError(w, r, http.StatusForbidden, "request.origin_denied")
		return
	}
	cookie, err := r.Cookie(s.cookieName)
	if err == nil {
		raw, _ := base64.RawURLEncoding.DecodeString(cookie.Value)
		if len(raw) != 32 || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf(raw))) {
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
