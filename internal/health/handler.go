// Package health provides bounded HTTP liveness and readiness handlers.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"
)

const (
	LivenessPath  = "/healthz"
	ReadinessPath = "/readyz"
)

var ErrInvalidOptions = errors.New("invalid health handler options")

var dependencyNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Pinger is the database capability required for readiness.
type Pinger interface {
	PingContext(context.Context) error
}

// Check validates one required or optional startup dependency. Check functions
// must honor the context deadline and return an error when unavailable.
type Check func(context.Context) error

type OptionalCheck struct {
	Name  string
	Check Check
}

type Options struct {
	Database         Pinger
	MigrationCheck   Check
	OptionalChecks   []OptionalCheck
	ReadinessTimeout time.Duration
}

// Handler serves only the built-in health routes. Database and migration
// verification are mandatory; optional checks affect degradation but not
// readiness. All readiness work shares one bounded request deadline.
type Handler struct {
	database       Pinger
	migrationCheck Check
	optional       []OptionalCheck
	timeout        time.Duration
}

func NewHandler(options Options) (*Handler, error) {
	if options.Database == nil || options.MigrationCheck == nil || options.ReadinessTimeout <= 0 {
		return nil, ErrInvalidOptions
	}
	optional := append([]OptionalCheck(nil), options.OptionalChecks...)
	seen := make(map[string]struct{}, len(optional))
	for _, check := range optional {
		if !dependencyNamePattern.MatchString(check.Name) || check.Check == nil {
			return nil, ErrInvalidOptions
		}
		if _, exists := seen[check.Name]; exists {
			return nil, ErrInvalidOptions
		}
		seen[check.Name] = struct{}{}
	}
	return &Handler{database: options.Database, migrationCheck: options.MigrationCheck, optional: optional, timeout: options.ReadinessTimeout}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, r, http.StatusMethodNotAllowed, response(w, map[string]any{
			"code":    "method.not_allowed",
			"message": "method is not allowed",
		}))
		return
	}
	switch r.URL.Path {
	case LivenessPath:
		writeJSON(w, r, http.StatusOK, response(w, map[string]any{
			"status":     "ok",
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		}))
	case ReadinessPath:
		h.readiness(w, r)
	default:
		writeJSON(w, r, http.StatusNotFound, response(w, map[string]any{
			"code":    "route.not_found",
			"message": "route was not found",
		}))
	}
}

func (h *Handler) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	dependencies := make(map[string]string, len(h.optional)+2)
	databaseErr := h.database.PingContext(ctx)
	if r.Context().Err() != nil {
		return
	}
	if databaseErr != nil || ctx.Err() != nil {
		dependencies["database"] = "unavailable"
		writeJSON(w, r, http.StatusServiceUnavailable, response(w, readinessError(dependencies)))
		return
	}
	dependencies["database"] = "available"
	migrationErr := h.migrationCheck(ctx)
	if r.Context().Err() != nil {
		return
	}
	if migrationErr != nil || ctx.Err() != nil {
		dependencies["migrations"] = "unavailable"
		writeJSON(w, r, http.StatusServiceUnavailable, response(w, readinessError(dependencies)))
		return
	}
	dependencies["migrations"] = "available"
	degraded := false
	for i, check := range h.optional {
		if r.Context().Err() != nil {
			return
		}
		if ctx.Err() != nil {
			degraded = true
			for _, pending := range h.optional[i:] {
				dependencies[pending.Name] = "unavailable"
			}
			break
		}
		checkErr := check.Check(ctx)
		if r.Context().Err() != nil {
			return
		}
		if checkErr != nil || ctx.Err() != nil {
			degraded = true
			dependencies[check.Name] = "unavailable"
			if ctx.Err() != nil {
				for _, pending := range h.optional[i+1:] {
					dependencies[pending.Name] = "unavailable"
				}
				break
			}
		} else {
			dependencies[check.Name] = "available"
		}
	}
	body := map[string]any{
		"status":       "ready",
		"checked_at":   time.Now().UTC().Format(time.RFC3339),
		"dependencies": dependencies,
	}
	if degraded {
		body["degraded"] = true
	}
	if r.Context().Err() != nil {
		return
	}
	writeJSON(w, r, http.StatusOK, response(w, body))
}

func readinessError(dependencies map[string]string) map[string]any {
	return map[string]any{
		"status":       "unavailable",
		"checked_at":   time.Now().UTC().Format(time.RFC3339),
		"dependencies": dependencies,
		"code":         "dependency.unavailable",
		"message":      "a required service is unavailable",
	}
}

// response carries the runtime's server-generated request ID into the body
// when this handler is mounted behind the runtime request-correlation layer.
func response(w http.ResponseWriter, body map[string]any) map[string]any {
	if requestID := w.Header().Get("X-Request-ID"); requestID != "" {
		body["request_id"] = requestID
	}
	return body
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(value)
	}
}
