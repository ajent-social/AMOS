// Package app exposes the composition seam used by generated AMOS applications.
package app

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	runtime "github.com/ajent-social/amos/internal/runtime"
)

const (
	defaultReadinessTimeout = 2 * time.Second
	defaultShutdownTimeout  = 5 * time.Second
)

var (
	// ErrInvalidOptions reports invalid application composition options.
	ErrInvalidOptions = runtime.ErrInvalidOptions
	// ErrInvalidRoute reports malformed method or route patterns.
	ErrInvalidRoute = runtime.ErrInvalidRoute
	// ErrReservedRoute reports an attempt to register an AMOS-owned path.
	ErrReservedRoute = runtime.ErrReservedRoute
	// ErrRouteConflict reports a duplicate or shadowing route registration.
	ErrRouteConflict = runtime.ErrRouteConflict
	// ErrAlreadyServing reports that this app already has an active HTTP server.
	ErrAlreadyServing = runtime.ErrAlreadyServing
	// ErrNotServing reports that shutdown was requested without an active server.
	ErrNotServing = runtime.ErrNotServing
)

// ReadinessCheck evaluates one required dependency using the request-bounded
// context supplied by AMOS. Return an error when the dependency is unavailable.
type ReadinessCheck func(context.Context) error

// IdentityHandlers binds only the selected AMOS authentication routes at startup.
// Handlers must enforce their own credential, origin and authorization checks.
// This constructor seam does not relax business route reservations.
type IdentityHandlers = runtime.IdentityHandlers

// Options configure the app's routes, required dependency checks, logging and
// HTTP lifecycle. A nil Logger uses slog.Default. Empty ReadinessChecks means
// the app declares no external readiness dependency.
type Options struct {
	Identity         IdentityHandlers
	Workspaces       http.Handler
	HealthHandler    http.Handler
	Logger           *slog.Logger
	ReadinessChecks  []ReadinessCheck
	ReadinessTimeout time.Duration
	ShutdownTimeout  time.Duration
}

// App is one composed AMOS HTTP application.
type App struct {
	runtime *runtime.Runtime
}

// New creates an app with AMOS-owned health, readiness, and reserved routes.
func New(options Options) (*App, error) {
	readinessTimeout := options.ReadinessTimeout
	if readinessTimeout == 0 {
		readinessTimeout = defaultReadinessTimeout
	}
	shutdownTimeout := options.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = defaultShutdownTimeout
	}
	if readinessTimeout < 0 || shutdownTimeout < 0 {
		return nil, ErrInvalidOptions
	}
	checks := make([]func(context.Context) error, len(options.ReadinessChecks))
	for index, check := range options.ReadinessChecks {
		if check == nil {
			return nil, ErrInvalidOptions
		}
		checks[index] = check
	}
	composed, err := runtime.New(runtime.Options{
		Identity:         options.Identity,
		Workspaces:       options.Workspaces,
		HealthHandler:    options.HealthHandler,
		Logger:           options.Logger,
		ReadinessChecks:  checks,
		ReadinessTimeout: readinessTimeout,
		ShutdownTimeout:  shutdownTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &App{runtime: composed}, nil
}

// RegisterBusinessRoute adds one exact route or a terminal prefix wildcard
// (for example, /reports/*). Method and path normalization is checked for
// duplicates, reserved paths, and wildcard shadowing.
func (a *App) RegisterBusinessRoute(method, pattern string, handler http.Handler) error {
	if a == nil || a.runtime == nil {
		return ErrInvalidOptions
	}
	return a.runtime.RegisterBusinessRoute(method, pattern, handler)
}

// Handler returns the same-origin HTTP handler with built-in shared routes.
func (a *App) Handler() http.Handler {
	if a == nil || a.runtime == nil {
		return http.NotFoundHandler()
	}
	return a.runtime
}

// Serve runs the app on listener until ctx is canceled or the server fails.
// Context cancellation triggers bounded graceful shutdown and then force-closes
// any requests still active at the configured shutdown deadline.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	if a == nil || a.runtime == nil {
		return ErrInvalidOptions
	}
	return a.runtime.Serve(ctx, listener)
}

// Shutdown gracefully stops the active server within ctx. If the deadline
// expires, active connections are force-closed before this method returns.
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil || a.runtime == nil {
		return ErrInvalidOptions
	}
	return a.runtime.Shutdown(ctx)
}
