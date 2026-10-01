// Package workspaceswitch renders and changes the browser's current workspace
// hint. Every operation is backed by a consumer supplied authoritative service.
package workspaceswitch

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/ajent-social/amos/identity"
	"github.com/google/uuid"
)

var (
	ErrDenied          = errors.New("workspace selection denied")
	ErrUnavailable     = errors.New("workspace selection unavailable")
	ErrInvalidSelector = errors.New("invalid workspace selector")
)

const hintCookie = "amos_workspace_hint"

// Workspace is display data returned only after the domain service has
// confirmed current authority. ID is a selector, never a source of authority.
type Workspace struct {
	ID   uuid.UUID
	Name string
	Kind string
}

// Service must resolve all methods from current authenticated state. Select
// performs the domain selection operation and must deny removed/suspended
// memberships and workspaces. Implementations must not trust cached claims.
type Service interface {
	// Choices returns the authoritative selectable workspaces and current
	// workspace from one consistent read. If the hint is absent or stale, it
	// returns the accessible choices with ErrDenied so the UI can remediate.
	Choices(context.Context, identity.Principal, uuid.UUID) (Workspace, []Workspace, error)
	Select(context.Context, identity.Principal, uuid.UUID) (Workspace, error)
}

// RequestCheck verifies request origin and CSRF proof for the selection POST.
// It is required; a missing check fails closed.
type RequestCheck interface {
	Valid(*http.Request) bool
	Token(*http.Request) (string, error)
}

type Handler struct {
	service Service
	check   RequestCheck
	page    *template.Template
}

// New constructs a handler with mandatory authoritative service and mutation
// verifier dependencies. There is intentionally no fallback implementation.
func New(service Service, check RequestCheck) (*Handler, error) {
	if service == nil || check == nil {
		return nil, ErrUnavailable
	}
	page, err := template.New("workspaces").Parse(pageHTML)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Handler{service: service, check: check, page: page}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil || h.check == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace.authorization_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Vary", "Cookie, HX-Request, HX-Target")
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth.unauthenticated")
		return
	}
	switch r.Method {
	case http.MethodGet:
		token, err := h.check.Token(r)
		if err != nil || token == "" {
			writeError(w, http.StatusServiceUnavailable, "request.csrf_unavailable")
			return
		}
		hint, err := hintFromRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		h.renderChoices(w, r, principal, hint, token)
	case http.MethodPost:
		if r.URL.Query().Has("workspace_id") || r.URL.Query().Has("next") {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/x-www-form-urlencoded" {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil || len(r.Form) != 2 || len(r.PostForm) != 2 {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) || errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
				return
			}
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		if !h.check.Valid(r) {
			writeError(w, http.StatusForbidden, "request.csrf_denied")
			return
		}
		values := r.PostForm["workspace_id"]
		csrf := r.PostForm["_csrf"]
		if len(values) != 1 || len(csrf) != 1 || strings.TrimSpace(values[0]) != values[0] {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		id, err := parseID(values[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, "workspace.selector_invalid")
			return
		}
		selected, err := h.service.Select(r.Context(), principal, id)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if selected.ID != id || selected.Kind == "" {
			writeError(w, http.StatusServiceUnavailable, "workspace.authorization_unavailable")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: hintCookie, Value: id.String(), Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 60 * 60 * 24 * 30})
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Refresh", "true")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "request.method_not_allowed")
	}
}

func (h *Handler) renderChoices(w http.ResponseWriter, r *http.Request, principal identity.Principal, hint uuid.UUID, token string) {
	current, items, currentErr := h.service.Choices(r.Context(), principal, hint)
	if errors.Is(currentErr, ErrDenied) && hint != uuid.Nil {
		// A denied snapshot may contain data from before membership changed.
		// Discard every returned field before retrying without the stale hint.
		http.SetCookie(w, &http.Cookie{Name: hintCookie, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		current, items, currentErr = h.service.Choices(r.Context(), principal, uuid.Nil)
	}
	h.renderWorkspacePage(w, current, items, currentErr, token)
}

func (h *Handler) renderWorkspacePage(w http.ResponseWriter, current Workspace, items []Workspace, snapshotErr error, token string) {
	if snapshotErr != nil {
		writeDomainError(w, snapshotErr)
		return
	}
	if !validChoices(current, items) {
		writeError(w, http.StatusForbidden, "workspace.denied")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := h.page.Execute(w, view{Current: current, Workspaces: items, CSRFToken: token}); err != nil {
		return
	}
}

type view struct {
	Current    Workspace
	Workspaces []Workspace
	CSRFToken  string
}

func hintFromRequest(r *http.Request) (uuid.UUID, error) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == hintCookie {
			value = cookie.Value
			count++
		}
	}
	if count > 1 {
		return uuid.Nil, ErrInvalidSelector
	}
	if count == 0 {
		return uuid.Nil, nil
	}
	return parseID(value)
}

func validChoices(current Workspace, items []Workspace) bool {
	if !validWorkspace(current) {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if !validWorkspace(item) {
			return false
		}
		if _, ok := seen[item.ID]; ok {
			return false
		}
		seen[item.ID] = struct{}{}
	}
	_, ok := seen[current.ID]
	return ok
}

func validWorkspace(workspace Workspace) bool {
	if workspace.ID == uuid.Nil || workspace.ID.Version() != 7 || workspace.ID.Variant() != uuid.RFC4122 || strings.TrimSpace(workspace.Name) == "" {
		return false
	}
	switch workspace.Kind {
	case "personal", "organization":
		return true
	default:
		return false
	}
}

func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.Version() != 7 || id.String() != raw {
		return uuid.Nil, errors.New("invalid workspace selector")
	}
	return id, nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDenied):
		writeError(w, http.StatusForbidden, "workspace.denied")
	default:
		writeError(w, http.StatusServiceUnavailable, "workspace.authorization_unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"` + code + `","message":"The request could not be authorized."}`))
}

const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Workspaces</title></head>
<body><a href="#main">Skip to main content</a><header><a href="/">AMOS</a><span aria-label="Current workspace">{{.Current.Name}}</span></header>
<main id="main"><h1>Workspaces</h1><p id="current-workspace">Current workspace: {{.Current.Name}}</p>
<form method="post" action="/workspaces" hx-post="/workspaces" hx-target="body" hx-swap="outerHTML"><input type="hidden" name="_csrf" value="{{.CSRFToken}}"><label for="workspace_id">Switch workspace</label><select id="workspace_id" name="workspace_id" required aria-describedby="workspace-help">
{{range .Workspaces}}<option value="{{.ID}}"{{if eq .ID $.Current.ID}} selected{{end}}>{{.Name}}</option>{{end}}</select><p id="workspace-help">Choose a workspace you currently belong to.</p>
<button type="submit">Switch workspace</button></form></main></body></html>`
