// Package runtime composes the public AMOS HTTP route surface.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/internal/telemetry"
)

var (
	ErrInvalidOptions = errors.New("invalid runtime options")
	ErrInvalidRoute   = errors.New("invalid business route")
	ErrReservedRoute  = errors.New("route is reserved by AMOS")
	ErrRouteConflict  = errors.New("business route conflicts with a registered route")
	ErrAlreadyServing = errors.New("runtime is already serving")
	ErrNotServing     = errors.New("runtime is not serving")
)

var reservedRoots = []string{"/signin", "/signout", "/signup", "/auth", "/verify-email", "/forgot-password", "/reset-password", "/oauth", "/.well-known", "/account", "/workspaces", "/billing", "/api", "/mcp", "/healthz", "/readyz"}

// Options configure route composition and HTTP lifecycle. A nil Logger uses
// slog.Default for one privacy-preserving record per request.
type Options struct {
	Identity         IdentityHandlers
	Workspaces       http.Handler
	HealthHandler    http.Handler
	Logger           *slog.Logger
	ReadinessChecks  []func(context.Context) error
	ReadinessTimeout time.Duration
	ShutdownTimeout  time.Duration
}

type route struct {
	method, pattern string
	wildcard        bool
	handler         http.Handler
}

type Runtime struct {
	identity         map[string]http.Handler
	mu               sync.RWMutex
	routes           []route
	healthHandler    http.Handler
	handler          http.Handler
	checks           []func(context.Context) error
	readinessTimeout time.Duration
	shutdownTimeout  time.Duration
	server           *http.Server
}

func New(opts Options) (*Runtime, error) {
	if opts.ReadinessTimeout <= 0 || opts.ShutdownTimeout <= 0 || (opts.HealthHandler != nil && len(opts.ReadinessChecks) != 0) {
		return nil, ErrInvalidOptions
	}
	for _, check := range opts.ReadinessChecks {
		if check == nil {
			return nil, ErrInvalidOptions
		}
	}
	routes := opts.Identity.routes()
	routes["GET /workspaces"] = opts.Workspaces
	routes["POST /workspaces"] = opts.Workspaces
	runtime := &Runtime{identity: routes, healthHandler: opts.HealthHandler, checks: append([]func(context.Context) error(nil), opts.ReadinessChecks...), readinessTimeout: opts.ReadinessTimeout, shutdownTimeout: opts.ShutdownTimeout}
	runtime.handler = telemetry.Logging(opts.Logger, runtime.routeTemplate, http.HandlerFunc(runtime.serveHTTP))
	return runtime, nil
}

func (r *Runtime) Register(method, pattern string, handler http.Handler) error {
	if r == nil || handler == nil {
		return ErrInvalidRoute
	}
	method = strings.ToUpper(method)
	if !validMethod(method) {
		return ErrInvalidRoute
	}
	wild := strings.HasSuffix(pattern, "/*")
	base := pattern
	if wild {
		base = strings.TrimSuffix(pattern, "/*")
	}
	canonical, err := normalizePath(base, true)
	if err != nil || canonical == "/" && wild {
		return ErrInvalidRoute
	}
	if wild {
		pattern = canonical + "/*"
	} else {
		pattern = canonical
	}
	if isReserved(canonical) {
		return ErrReservedRoute
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.routes {
		if existing.method == method && patternsOverlap(existing, route{method: method, pattern: pattern, wildcard: wild}) {
			return ErrRouteConflict
		}
	}
	r.routes = append(r.routes, route{method: method, pattern: pattern, wildcard: wild, handler: handler})
	return nil
}

func (r *Runtime) RegisterBusinessRoute(method, pattern string, handler http.Handler) error {
	return r.Register(method, pattern, handler)
}

func patternsOverlap(a, b route) bool {
	abase, bbase := strings.TrimSuffix(a.pattern, "/*"), strings.TrimSuffix(b.pattern, "/*")
	if !a.wildcard && !b.wildcard {
		return abase == bbase
	}
	if a.wildcard && b.wildcard {
		return abase == bbase || strings.HasPrefix(abase, bbase+"/") || strings.HasPrefix(bbase, abase+"/")
	}
	if a.wildcard {
		return strings.HasPrefix(bbase, abase+"/")
	}
	return strings.HasPrefix(abase, bbase+"/")
}

func validMethod(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		letter := c >= 'A' && c <= 'Z'
		digit := c >= '0' && c <= '9'
		punctuation := strings.ContainsRune("!#$%&'*+-.^_`|~", c)
		if !letter && !digit && !punctuation {
			return false
		}
	}
	return true
}

func normalizePath(raw string, registration bool) (string, error) {
	if raw == "" || !utf8.ValidString(raw) || raw[0] != '/' || strings.ContainsAny(raw, "?#\\") || strings.Contains(raw, "//") {
		return "", ErrInvalidRoute
	}
	if registration && strings.Contains(raw, "%") {
		return "", ErrInvalidRoute
	}
	if strings.Contains(strings.ToLower(raw), "%2f") || strings.Contains(strings.ToLower(raw), "%5c") {
		return "", ErrInvalidRoute
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "?#\\%") || strings.Contains(decoded, "//") {
		return "", ErrInvalidRoute
	}
	for _, r := range decoded {
		if unicode.IsControl(r) {
			return "", ErrInvalidRoute
		}
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == "." || part == ".." {
			return "", ErrInvalidRoute
		}
	}
	if len(decoded) > 1 {
		decoded = strings.TrimSuffix(decoded, "/")
	}
	if decoded == "" {
		decoded = "/"
	}
	return decoded, nil
}

func isReserved(path string) bool {
	lower := strings.ToLower(path)
	for _, root := range reservedRoots {
		if lower == root || strings.HasPrefix(lower, root+"/") {
			return true
		}
	}
	return false
}

func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// An explicitly composed outer telemetry middleware already owns the
	// request ID and log record. Do not nest another logger or replace its ID.
	if telemetry.RequestID(req.Context()) != "" {
		r.serveHTTP(w, req)
		return
	}
	r.handler.ServeHTTP(w, req)
}

func (r *Runtime) routeTemplate(req *http.Request) string {
	if r == nil || req == nil || req.URL == nil {
		return ""
	}
	raw := req.RequestURI
	if raw == "" {
		raw = req.URL.EscapedPath()
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	path, err := normalizePath(raw, false)
	if err != nil {
		return ""
	}
	if path == "/healthz" || path == "/readyz" {
		return path
	}
	if isReserved(path) {
		if _, ok := r.identity[req.Method+" "+path]; ok {
			return path
		}
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, candidate := range r.routes {
		if candidate.method == req.Method && (candidate.pattern == path || candidate.wildcard && strings.HasPrefix(path, strings.TrimSuffix(candidate.pattern, "*"))) {
			return candidate.pattern
		}
	}
	return ""
}

func (r *Runtime) serveHTTP(w http.ResponseWriter, req *http.Request) {
	requestID := telemetry.RequestID(req.Context())
	w.Header().Set("X-Request-ID", requestID)
	raw := req.RequestURI
	if raw == "" {
		raw = req.URL.EscapedPath()
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	path, err := normalizePath(raw, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, requestID, "path.invalid", "request path is ambiguous")
		return
	}
	if isReserved(path) {
		if path == "/healthz" || path == "/readyz" {
			if req.Method != http.MethodGet && req.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed, requestID, "method.not_allowed", "method is not allowed")
				return
			}
			if r.healthHandler != nil {
				r.healthHandler.ServeHTTP(w, req)
				return
			}
			if path == "/healthz" {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
			r.readiness(w, req)
			return
		}
		if handler := r.identity[req.Method+" "+path]; handler != nil {
			handler.ServeHTTP(w, req)
			return
		}
		writeError(w, http.StatusServiceUnavailable, requestID, "route.unavailable", "this AMOS route is reserved but unavailable")
		return
	}
	r.mu.RLock()
	var handler http.Handler
	for _, candidate := range r.routes {
		if candidate.method == req.Method && (candidate.pattern == path || candidate.wildcard && strings.HasPrefix(path, strings.TrimSuffix(candidate.pattern, "*"))) {
			handler = candidate.handler
			break
		}
	}
	r.mu.RUnlock()
	if handler != nil {
		handler.ServeHTTP(w, req)
		return
	}
	writeError(w, http.StatusNotFound, requestID, "route.not_found", "route was not found")
}

func (r *Runtime) readiness(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), r.readinessTimeout)
	defer cancel()
	for _, check := range r.checks {
		if err := check(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, w.Header().Get("X-Request-ID"), "dependency.unavailable", "a required service is unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeError(w http.ResponseWriter, status int, requestID, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message, "request_id": requestID})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (r *Runtime) Serve(ctx context.Context, listener net.Listener) error {
	if ctx == nil || listener == nil {
		return ErrInvalidOptions
	}
	r.mu.Lock()
	if r.server != nil {
		r.mu.Unlock()
		return ErrAlreadyServing
	}
	srv := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	r.server = srv
	r.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	select {
	case err := <-done:
		r.clearServer(srv)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			_ = srv.Close()
		}
		serveErr := <-done
		r.clearServer(srv)
		if err != nil {
			return fmt.Errorf("shutdown runtime: %w", err)
		}
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}

func (r *Runtime) clearServer(s *http.Server) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.server == s {
		r.server = nil
	}
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidOptions
	}
	r.mu.RLock()
	srv := r.server
	r.mu.RUnlock()
	if srv == nil {
		return ErrNotServing
	}
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown runtime: %w", err)
	}
	return nil
}
