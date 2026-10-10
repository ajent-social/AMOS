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
	"reflect"
	"sort"
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
	ErrRoutesFrozen   = errors.New("runtime routes are frozen")
)

var reservedRoots = []string{"/signin", "/signout", "/signup", "/auth", "/verify-email", "/forgot-password", "/reset-password", "/oauth", "/.well-known", "/account", "/workspaces", "/billing", "/api", "/mcp", "/healthz", "/readyz"}

// Options configure route composition and HTTP lifecycle. A nil Logger uses
// slog.Default for one privacy-preserving record per request.
type Options struct {
	Identity         IdentityHandlers
	Workspaces       http.Handler
	Billing          http.Handler
	Protocol         *ProtocolHandlersV1
	Business         *BusinessRoutesV1
	HealthHandler    http.Handler
	Logger           *slog.Logger
	ReadinessChecks  []func(context.Context) error
	ReadinessTimeout time.Duration
	ShutdownTimeout  time.Duration
}

// ProtocolHandlersV1 binds the fixed, reserved MCP/OAuth protocol slots.
type ProtocolHandlersV1 struct {
	MCP                         http.Handler
	AuthorizationServerMetadata http.Handler
	ProtectedResourceMetadata   http.Handler
	Register                    http.Handler
	Authorize                   http.Handler
	Consent                     http.Handler
	Token                       http.Handler
	Revoke                      http.Handler
}

// BusinessRouteV1 binds one exact canonical path or terminal prefix.
type BusinessRouteV1 struct {
	Pattern string
	Handler http.Handler
}

// BusinessRoutesV1 is a finite method-neutral business route manifest.
type BusinessRoutesV1 struct {
	Home   http.Handler
	Routes []BusinessRouteV1
}

type route struct {
	method, pattern string
	wildcard        bool
	handler         http.Handler
}

type pathRoute struct {
	pattern  string
	base     string
	wildcard bool
	handler  http.Handler
}

type selectedRoute struct {
	path       string
	pathError  bool
	aliasError bool
	health     bool
	template   string
	handler    http.Handler
	reserved   bool
}

type Runtime struct {
	identity         map[string]http.Handler
	mu               sync.RWMutex
	routes           []route
	business         []pathRoute
	businessExact    map[string]http.Handler
	protocol         map[string]http.Handler
	billing          http.Handler
	home             http.Handler
	routesFrozen     bool
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
	protocol, err := copyProtocol(opts.Protocol)
	if err != nil {
		return nil, err
	}
	business, home, err := copyBusiness(opts.Business)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{identity: routes, business: business, home: home, businessExact: make(map[string]http.Handler), protocol: protocol, billing: opts.Billing, healthHandler: opts.HealthHandler, checks: append([]func(context.Context) error(nil), opts.ReadinessChecks...), readinessTimeout: opts.ReadinessTimeout, shutdownTimeout: opts.ShutdownTimeout}
	for _, candidate := range business {
		if candidate.wildcard {
			continue
		}
		runtime.businessExact[candidate.pattern] = candidate.handler
	}
	sort.Slice(runtime.business, func(i, j int) bool {
		if runtime.business[i].wildcard != runtime.business[j].wildcard {
			return !runtime.business[i].wildcard
		}
		return len(runtime.business[i].base) > len(runtime.business[j].base)
	})
	runtime.handler = telemetry.Logging(opts.Logger, runtime.routeTemplate, http.HandlerFunc(runtime.serveHTTP))
	return runtime, nil
}

var protocolPaths = []string{
	"/mcp",
	"/.well-known/oauth-authorization-server",
	"/.well-known/oauth-protected-resource/mcp",
	"/oauth/register",
	"/oauth/authorize",
	"/oauth/consent",
	"/oauth/token",
	"/oauth/revoke",
}

func invalidHandler(handler http.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func copyProtocol(input *ProtocolHandlersV1) (map[string]http.Handler, error) {
	if input == nil {
		return nil, nil
	}
	copy := *input
	handlers := []http.Handler{copy.MCP, copy.AuthorizationServerMetadata, copy.ProtectedResourceMetadata, copy.Register, copy.Authorize, copy.Consent, copy.Token, copy.Revoke}
	for _, handler := range handlers {
		if invalidHandler(handler) {
			return nil, ErrInvalidOptions
		}
	}
	result := make(map[string]http.Handler, len(protocolPaths))
	for index, path := range protocolPaths {
		result[path] = handlers[index]
	}
	return result, nil
}

func copyBusiness(input *BusinessRoutesV1) ([]pathRoute, http.Handler, error) {
	if input == nil {
		return nil, nil, nil
	}
	copy := *input
	copy.Routes = append([]BusinessRouteV1(nil), input.Routes...)
	if invalidHandler(copy.Home) && len(copy.Routes) == 0 {
		return nil, nil, ErrInvalidOptions
	}
	if copy.Home != nil && invalidHandler(copy.Home) {
		return nil, nil, ErrInvalidOptions
	}
	compiled := make([]pathRoute, 0, len(copy.Routes))
	seen := make(map[string]struct{}, len(copy.Routes))
	for _, configured := range copy.Routes {
		if invalidHandler(configured.Handler) {
			return nil, nil, ErrInvalidOptions
		}
		wildcard := strings.HasSuffix(configured.Pattern, "/*")
		base := configured.Pattern
		if wildcard {
			base = strings.TrimSuffix(base, "/*")
		}
		canonical, err := normalizePath(base, true)
		if err != nil || canonical == "/" || strings.HasSuffix(base, "/") && base != "/" {
			return nil, nil, ErrInvalidRoute
		}
		if wildcard && strings.Contains(strings.TrimSuffix(configured.Pattern, "/*"), "*") || !wildcard && strings.Contains(configured.Pattern, "*") {
			return nil, nil, ErrInvalidRoute
		}
		if canonical != base || isReserved(canonical) {
			if isReserved(canonical) {
				return nil, nil, ErrReservedRoute
			}
			return nil, nil, ErrInvalidRoute
		}
		pattern := canonical
		if wildcard {
			pattern += "/*"
		}
		if _, exists := seen[pattern]; exists {
			return nil, nil, ErrRouteConflict
		}
		seen[pattern] = struct{}{}
		compiled = append(compiled, pathRoute{pattern: pattern, base: canonical, wildcard: wildcard, handler: configured.Handler})
	}
	return compiled, copy.Home, nil
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
	if r.routesFrozen {
		return ErrRoutesFrozen
	}
	if pattern == "/" && r.home != nil && !wild {
		return ErrRouteConflict
	}
	for _, existing := range r.routes {
		if existing.method == method && patternsOverlap(existing, route{method: method, pattern: pattern, wildcard: wild}) {
			return ErrRouteConflict
		}
	}
	legacy := route{method: method, pattern: pattern, wildcard: wild, handler: handler}
	for _, candidate := range r.business {
		if pathPatternsOverlap(candidate, legacy) {
			return ErrRouteConflict
		}
	}
	r.routes = append(r.routes, route{method: method, pattern: pattern, wildcard: wild, handler: handler})
	return nil
}

func pathPatternsOverlap(business pathRoute, legacy route) bool {
	legacyBase := strings.TrimSuffix(legacy.pattern, "/*")
	if !business.wildcard && !legacy.wildcard {
		return business.base == legacyBase
	}
	if !business.wildcard && legacy.wildcard {
		return strings.HasPrefix(business.base, legacyBase+"/")
	}
	if business.wildcard && !legacy.wildcard {
		return legacyBase == business.base || strings.HasPrefix(legacyBase, business.base+"/")
	}
	return business.base == legacyBase || strings.HasPrefix(business.base, legacyBase+"/") || strings.HasPrefix(legacyBase, business.base+"/")
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

func isBillingPath(path string) bool {
	return path == "/billing" || strings.HasPrefix(path, "/billing/")
}

func isReserved(path string) bool {
	lower := strings.ToLower(path)
	if isBillingPath(lower) {
		return true
	}
	for _, root := range reservedRoots {
		if lower == root || strings.HasPrefix(lower, root+"/") {
			return true
		}
	}
	return false
}

func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || req == nil {
		http.NotFoundHandler().ServeHTTP(w, req)
		return
	}
	r.freezeRoutes()
	// An explicitly composed outer telemetry middleware already owns the
	// request ID and log record. Do not nest another logger or replace its ID.
	if telemetry.RequestID(req.Context()) != "" {
		r.serveHTTP(w, req)
		return
	}
	r.handler.ServeHTTP(w, req)
}

func (r *Runtime) routeTemplate(req *http.Request) string {
	return r.selectRoute(req).template
}

func (r *Runtime) serveHTTP(w http.ResponseWriter, req *http.Request) {
	requestID := telemetry.RequestID(req.Context())
	w.Header().Set("X-Request-ID", requestID)
	selected := r.selectRoute(req)
	if selected.pathError || selected.aliasError {
		writeError(w, http.StatusBadRequest, requestID, "path.invalid", "request path is ambiguous")
		return
	}
	if selected.health {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, requestID, "method.not_allowed", "method is not allowed")
			return
		}
		if r.healthHandler != nil {
			r.healthHandler.ServeHTTP(w, req)
			return
		}
		if selected.path == "/healthz" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		r.readiness(w, req)
		return
	}
	if selected.reserved {
		writeError(w, http.StatusServiceUnavailable, requestID, "route.unavailable", "this AMOS route is reserved but unavailable")
		return
	}
	if selected.handler != nil {
		selected.handler.ServeHTTP(w, req)
		return
	}
	writeError(w, http.StatusNotFound, requestID, "route.not_found", "route was not found")
}

func (r *Runtime) selectRoute(req *http.Request) selectedRoute {
	selected := selectedRoute{}
	if r == nil || req == nil || req.URL == nil {
		selected.pathError = true
		return selected
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
		selected.pathError = true
		return selected
	}
	selected.path = path
	if raw != path && (isProtocolPath(path) || isBillingPath(path) || r.matchesBusinessPath(path)) {
		selected.aliasError = true
		return selected
	}
	if path == "/healthz" || path == "/readyz" {
		selected.health = true
		selected.template = path
		return selected
	}
	if isBillingPath(path) {
		r.mu.RLock()
		selected.handler = r.billing
		r.mu.RUnlock()
		selected.template = path
		selected.reserved = selected.handler == nil
		return selected
	}
	if isReserved(path) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if handler, ok := r.identity[req.Method+" "+path]; ok {
			selected.handler = handler
			selected.template = path
			selected.reserved = handler == nil
			return selected
		}
		if isProtocolPath(path) {
			handler := r.protocol[path]
			selected.handler = handler
			selected.template = path
			selected.reserved = handler == nil
			return selected
		}
		selected.reserved = true
		return selected
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if path == "/" && r.home != nil {
		selected.handler = r.home
		selected.template = "/"
		return selected
	}
	if handler := r.businessExact[path]; handler != nil {
		selected.handler = handler
		selected.template = path
		return selected
	}
	for _, candidate := range r.business {
		if candidate.wildcard && (path == candidate.base || strings.HasPrefix(path, candidate.base+"/")) {
			selected.handler = candidate.handler
			selected.template = candidate.pattern
			return selected
		}
	}
	for _, candidate := range r.routes {
		if candidate.method == req.Method && (candidate.pattern == path || candidate.wildcard && strings.HasPrefix(path, strings.TrimSuffix(candidate.pattern, "*"))) {
			selected.handler = candidate.handler
			selected.template = candidate.pattern
			return selected
		}
	}
	return selected
}

func isProtocolPath(path string) bool {
	for _, candidate := range protocolPaths {
		if path == candidate {
			return true
		}
	}
	return false
}

func (r *Runtime) matchesBusinessPath(path string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if path == "/" && r.home != nil {
		return true
	}
	if _, ok := r.businessExact[path]; ok {
		return true
	}
	for _, candidate := range r.business {
		if candidate.wildcard && (path == candidate.base || strings.HasPrefix(path, candidate.base+"/")) {
			return true
		}
	}
	return false
}

func (r *Runtime) freezeRoutes() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.routesFrozen = true
	r.mu.Unlock()
}

// Handler freezes route registration and returns the runtime handler.
func (r *Runtime) Handler() http.Handler {
	r.freezeRoutes()
	if r == nil {
		return http.NotFoundHandler()
	}
	return r
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
		r.routesFrozen = true
		r.mu.Unlock()
		return ErrAlreadyServing
	}
	r.routesFrozen = true
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
