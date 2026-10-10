// Package context resolves an untrusted workspace selection against the
// authenticated person and current PostgreSQL state at each request boundary.
package context

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

var (
	ErrInvalidConfiguration = errors.New("invalid workspace context configuration")
	ErrInvalidSelector      = errors.New("invalid workspace selector")
	ErrDenied               = errors.New("workspace selection denied")
	ErrUnavailable          = errors.New("workspace authorization unavailable")
)

type Config struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
}

// Selection contains only values read from current identity/workspace rows.
// Permissions and epochs are snapshots for the current request, never caches.
type Selection struct {
	Workspace       workspacestore.Workspace
	Membership      *workspacestore.Membership
	Permissions     []string
	MembershipEpoch int64
}

type Resolver struct {
	db  *storage.DB
	cfg Config
}

type selectionKey struct{}
type hintKey struct{}

const workspaceHintCookie = "amos_workspace_hint"

// WithWorkspaceHint stores only an untrusted selection hint for the current
// request context. Resolver.Middleware always revalidates it from PostgreSQL.
func WithWorkspaceHint(ctx context.Context, workspaceID uuid.UUID) context.Context {
	return context.WithValue(ctx, hintKey{}, workspaceID)
}

func FromContext(ctx context.Context) (Selection, bool) {
	if ctx == nil {
		return Selection{}, false
	}
	s, ok := ctx.Value(selectionKey{}).(Selection)
	if !ok {
		return Selection{}, false
	}
	return copySelection(s), true
}

func New(db *storage.DB, cfg Config) (*Resolver, error) {
	if db == nil || !validConfig(cfg) {
		return nil, ErrInvalidConfiguration
	}
	return &Resolver{db: db, cfg: cfg}, nil
}

func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r == nil || r.db == nil || next == nil {
			writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		if hasIdentityHeader(req.Header) {
			writeError(w, http.StatusBadRequest, "request.identity_header_denied")
			return
		}
		principal, ok := identity.PrincipalFromContext(req.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "auth.unauthenticated")
			return
		}
		if principal.InstallationID() != r.cfg.InstallationID ||
			principal.ApplicationID() != r.cfg.ApplicationID ||
			principal.EnvironmentID() != r.cfg.EnvironmentID || principal.SecurityEpoch() < 0 {
			writeError(w, http.StatusForbidden, "workspace.denied")
			return
		}
		actor := principal.Actor()
		if actor.Kind() == "machine" {
			// Machine grant resolution is intentionally unavailable until the
			// current-owner authority adapter is qualified by its owning task.
			writeError(w, http.StatusServiceUnavailable, "workspace.authorization_unavailable")
			return
		}
		if actor.Kind() != "person" || actor.PersonID() == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "auth.unauthenticated")
			return
		}
		workspaceID, err := requestedWorkspace(req)
		if errors.Is(err, ErrInvalidSelector) {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "workspace.selector_required")
			return
		}
		selection, err := r.resolve(req.Context(), principal, workspaceID)
		if errors.Is(err, ErrDenied) {
			// The same response covers unknown, cross-scope, suspended and
			// unauthorized workspaces so callers cannot enumerate tenant IDs.
			writeError(w, http.StatusForbidden, "workspace.denied")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "workspace.authorization_unavailable")
			return
		}
		if cookie := currentHintCookie(req); cookie != selection.Workspace.ID.String() {
			http.SetCookie(w, &http.Cookie{
				Name: workspaceHintCookie, Value: selection.Workspace.ID.String(),
				Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
			})
		}
		ctx := context.WithValue(req.Context(), selectionKey{}, selection)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func (r *Resolver) resolve(ctx context.Context, principal identity.Principal, workspaceID uuid.UUID) (Selection, error) {
	var selection Selection
	err := r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		personID := principal.Actor().PersonID()
		var workspace workspacestore.Workspace
		var personState string
		var securityEpoch int64
		err := tx.QueryRowContext(ctx, `
			SELECT w.id, w.installation_id, w.application_id, w.kind, w.state,
			       w.personal_owner_id, w.workspace_epoch, w.created_at, w.updated_at,
			       p.state, p.security_epoch
			FROM workspaces w
			JOIN identity_persons p
			  ON p.id = $4 AND p.installation_id = w.installation_id
			 AND p.application_id = w.application_id
			WHERE w.id = $1 AND w.installation_id = $2 AND w.application_id = $3`,
			workspaceID, r.cfg.InstallationID, r.cfg.ApplicationID, personID).Scan(
			&workspace.ID, &workspace.Scope.InstallationID, &workspace.Scope.ApplicationID,
			&workspace.Kind, &workspace.State, &workspace.PersonalOwnerID,
			&workspace.Epoch, &workspace.CreatedAt, &workspace.UpdatedAt,
			&personState, &securityEpoch)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDenied
		}
		if err != nil {
			return err
		}
		if personState != "active" || securityEpoch != principal.SecurityEpoch() || workspace.State != workspacestore.WorkspaceActive {
			return ErrDenied
		}
		selection.Workspace = workspace
		switch workspace.Kind {
		case workspacestore.KindPersonal:
			if !personalOwner(workspace, personID) {
				return ErrDenied
			}
		case workspacestore.KindOrganization:
			var membership workspacestore.Membership
			err = tx.QueryRowContext(ctx, `
				SELECT m.id, m.installation_id, m.application_id, m.workspace_id,
				       m.person_id, m.role_key, m.role_version, m.state,
			       m.membership_epoch, m.created_at, m.updated_at, rv.permissions
			FROM workspace_memberships m
			JOIN workspace_role_versions rv
			  ON rv.role_key = m.role_key AND rv.version = m.role_version
			WHERE m.installation_id = $1 AND m.application_id = $2
			  AND m.workspace_id = $3 AND m.person_id = $4 AND m.state = 'active'`,
				r.cfg.InstallationID, r.cfg.ApplicationID, workspaceID, personID).Scan(
				&membership.ID, &membership.Scope.InstallationID, &membership.Scope.ApplicationID,
				&membership.WorkspaceID, &membership.PersonID, &membership.Role,
				&membership.RoleVersion, &membership.State, &membership.Epoch,
				&membership.CreatedAt, &membership.UpdatedAt, &selection.Permissions)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrDenied
			}
			if err != nil {
				return err
			}
			selection.Membership = &membership
			selection.MembershipEpoch = membership.Epoch
		default:
			return ErrDenied
		}
		return nil
	})
	if errors.Is(err, ErrDenied) {
		return Selection{}, ErrDenied
	}
	if err != nil {
		return Selection{}, ErrUnavailable
	}
	return copySelection(selection), nil
}

func requestedWorkspace(req *http.Request) (uuid.UUID, error) {
	var hinted uuid.UUID
	if value, ok := req.Context().Value(hintKey{}).(uuid.UUID); ok {
		hinted = value
	}
	values := req.Header.Values("X-Workspace-ID")
	if len(values) > 1 {
		return uuid.Nil, ErrInvalidSelector
	}
	var fromHeader uuid.UUID
	if len(values) == 1 {
		parsed, err := parseID(strings.TrimSpace(values[0]))
		if err != nil {
			return uuid.Nil, ErrInvalidSelector
		}
		fromHeader = parsed
	}
	// An explicit request can replace an older or malformed browser hint. The
	// selected target is still checked against authoritative SQL before use.
	if fromHeader != uuid.Nil {
		return fromHeader, nil
	}
	cookieValues := make([]string, 0, 1)
	for _, cookie := range req.Cookies() {
		if cookie.Name == workspaceHintCookie {
			cookieValues = append(cookieValues, cookie.Value)
		}
	}
	if len(cookieValues) > 1 {
		return uuid.Nil, ErrInvalidSelector
	}
	var fromCookie uuid.UUID
	if len(cookieValues) == 1 {
		parsed, err := parseID(cookieValues[0])
		if err != nil {
			return uuid.Nil, ErrInvalidSelector
		}
		fromCookie = parsed
	}
	// A direct selector allows a user to switch from the previously stored
	// hint. Both are untrusted; only the SQL resolution grants access.
	if hinted != uuid.Nil {
		if !validID(hinted) {
			return uuid.Nil, ErrInvalidSelector
		}
		return hinted, nil
	}
	if fromCookie != uuid.Nil {
		return fromCookie, nil
	}
	return uuid.Nil, ErrDenied
}

func currentHintCookie(req *http.Request) string {
	for _, cookie := range req.Cookies() {
		if cookie.Name == workspaceHintCookie {
			return cookie.Value
		}
	}
	return ""
}

func hasIdentityHeader(header http.Header) bool {
	for key, values := range header {
		canonical := http.CanonicalHeaderKey(key)
		if len(values) == 0 || strings.EqualFold(canonical, "X-Workspace-ID") {
			continue
		}
		switch strings.ToLower(canonical) {
		case "x-person-id", "x-actor-id", "x-principal-id", "x-identity-id",
			"x-machine-id", "x-tenant-id", "x-organization-id",
			"x-installation-id", "x-application-id", "x-environment-id",
			"x-amos-person-id", "x-amos-tenant-id":
			return true
		}
	}
	return false
}

func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id.String() != raw || !validID(id) {
		return uuid.Nil, ErrInvalidSelector
	}
	return id, nil
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == strings.ToLower(id.String())
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: "The request could not be authorized."})
}
