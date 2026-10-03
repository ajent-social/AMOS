package app

import (
	"encoding/json"
	"net/http"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
)

// OwnerRouteHandler handles a business request only after the native AMOS
// session has been revalidated and bound to the configured owner person.
// The principal is immutable and comes from trusted session middleware.
type OwnerRouteHandler func(http.ResponseWriter, *http.Request, identity.Principal)

// RegisterOwnerBusinessRoute registers a business route protected by AMOS's
// current, scope-bound browser session. expectedOwnerPersonID is configuration
// chosen by the application, never request data. Unsafe methods also retain the
// session middleware's exact-origin and CSRF checks.
func (a *App) RegisterOwnerBusinessRoute(method, pattern string, sessions *session.Service, expectedOwnerPersonID identity.ID, handler OwnerRouteHandler) error {
	if a == nil || a.runtime == nil || sessions == nil || !validOwnerPersonID(expectedOwnerPersonID) || handler == nil {
		return ErrInvalidOptions
	}
	protected := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok || principal.Actor().Kind() != "person" || principal.PersonID() != expectedOwnerPersonID {
			writeOwnerRouteDenied(w)
			return
		}
		handler(w, r, principal)
	}))
	return a.RegisterBusinessRoute(method, pattern, protected)
}

func validOwnerPersonID(id identity.ID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func writeOwnerRouteDenied(w http.ResponseWriter) {
	requestID := w.Header().Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
		w.Header().Set("X-Request-ID", requestID)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code": "auth.forbidden", "message": "request is not authorized", "request_id": requestID,
	})
}
