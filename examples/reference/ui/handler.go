// Package ui provides the server-rendered reference landing and todo pages.
package ui

import (
	"crypto/subtle"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ajent-social/amos/examples/reference/app/business"
	"github.com/ajent-social/amos/examples/reference/app/business/apigen"
)

//go:embed ui.css htmx.min.js csrf.js
var stylesheet embed.FS

// Todos is the public domain seam consumed by the reference pages. The request
// context is passed through unchanged so the host can attach the authenticated
// principal before this handler runs.
type Todos interface {
	apigen.OpTodosCreateHandler
	apigen.OpTodosListHandler
	apigen.OpTodosUpdateHandler
}

// Options resolves the session-bound CSRF token for the current request.
// When it is absent or cannot provide a token, mutation forms are hidden and
// mutation requests fail closed.
type Options struct {
	CSRFToken func(*http.Request) (string, bool)
}

const maxFormBodyBytes = 16 << 10

// NewHandler returns the reference landing and todo routes. The host remains
// responsible for authentication, workspace selection, and mounting the routes.
func NewHandler(todos Todos, options ...Options) http.Handler {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	h := &handler{todos: todos, csrfToken: option.CSRFToken}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.home)
	mux.HandleFunc("GET /reference/ui.css", h.styles)
	mux.HandleFunc("GET /reference/htmx.min.js", h.htmx)
	mux.HandleFunc("GET /reference/csrf.js", h.csrfScript)
	mux.HandleFunc("GET /todos", h.list)
	mux.HandleFunc("POST /todos", h.create)
	mux.HandleFunc("GET /todos/{id}/edit", h.edit)
	mux.HandleFunc("POST /todos/{id}/edit", h.update)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		mux.ServeHTTP(w, r)
	})
}

type handler struct {
	todos     Todos
	csrfToken func(*http.Request) (string, bool)
}

type pageData struct {
	Title, Workspace, Message, Error, TodoID string
	CSRF                                     string
	CanMutate                                bool
	Todos                                    []todoView
	Editing                                  *todoView
}

type todoView struct {
	ID, Title string
	Completed bool
	Revision  int64
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="csrf-token" content="{{.CSRF}}"><title>{{.Title}} · AMOS reference</title><link rel="stylesheet" href="/reference/ui.css"><script src="/reference/htmx.min.js" defer></script><script src="/reference/csrf.js" defer></script></head>
<body><a class="skip" href="#main">Skip to content</a><header><a href="/">AMOS reference</a><nav><a href="/todos?workspace={{urlquery .Workspace}}">Todos</a></nav></header><main id="main">
{{if .Message}}<p role="status" class="notice">{{.Message}}</p>{{end}}{{if .Error}}<p role="alert" class="error">{{.Error}}</p>{{end}}
{{if eq .Title "Your work, in one place"}}<section class="hero"><p class="eyebrow">A small business workspace</p><h1>Your work, in one place</h1><p>Keep a shared list of the next things to do.</p><form action="/todos" method="get"><label for="home-workspace">Workspace ID</label><div class="row"><input id="home-workspace" name="workspace" value="{{.Workspace}}" required autocomplete="off"><button type="submit">Open todos</button></div></form></section>
{{else}}<section><p class="eyebrow">Reference workspace</p><h1>Todos</h1><form action="/todos" method="get"><label for="workspace">Workspace ID</label><div class="row"><input id="workspace" name="workspace" value="{{.Workspace}}" required autocomplete="off"><button type="submit">Load workspace</button></div></form>
{{if .Workspace}}<section class="card"><h2>{{if .Editing}}Edit todo{{else}}Add a todo{{end}}</h2>{{if .CanMutate}}{{if .Editing}}<form action="/todos/{{.Editing.ID}}/edit" method="post" hx-boost="true"><input type="hidden" name="_csrf" value="{{.CSRF}}"><input type="hidden" name="workspace" value="{{.Workspace}}"><input type="hidden" name="revision" value="{{.Editing.Revision}}"><label for="title">Title</label><input id="title" name="title" value="{{.Editing.Title}}" required maxlength="200"><label><input type="checkbox" name="completed" value="true" {{if .Editing.Completed}}checked{{end}}> Completed</label><button type="submit">Save changes</button><a href="/todos?workspace={{urlquery .Workspace}}">Cancel</a></form>{{else}}<form action="/todos" method="post" hx-boost="true"><input type="hidden" name="_csrf" value="{{.CSRF}}"><input type="hidden" name="workspace" value="{{.Workspace}}"><label for="title">New todo</label><div class="row"><input id="title" name="title" required maxlength="200" autocomplete="off"><button type="submit">Add todo</button></div></form>{{end}}{{else}}<p role="status" class="muted">Changes are unavailable until a protected session is ready.</p>{{end}}</section>
<section class="card" aria-labelledby="todo-heading"><h2 id="todo-heading">Your list</h2>{{if .Todos}}<ul class="todos">{{range .Todos}}<li><span class="state">{{if .Completed}}Complete{{else}}Open{{end}}</span><span class="todo-title">{{.Title}}</span><a href="/todos/{{.ID}}/edit?workspace={{urlquery $.Workspace}}">Edit</a></li>{{end}}</ul>{{else}}<p>No todos yet. Add one above to get started.</p>{{end}}</section>{{else}}<p class="muted">Enter a workspace ID to see its todos.</p>{{end}}</section>{{end}}
</main><footer>AMOS reference interface</footer></body></html>`))

func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, pageData{Title: "Your work, in one place", Workspace: r.URL.Query().Get("workspace"), Message: r.URL.Query().Get("message")})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspace")
	data := pageData{Title: "Todos", Workspace: workspace, Message: r.URL.Query().Get("message")}
	if workspace == "" {
		h.render(w, http.StatusOK, data)
		return
	}
	h.mutationState(r, &data)
	if h.todos == nil {
		data.Error = "Todos are temporarily unavailable. Please try again later."
		h.render(w, http.StatusServiceUnavailable, data)
		return
	}
	result, err := h.todos.HandleTodosList(r.Context(), apigen.OpTodosListRequest{Body: apigen.ModelListTodos{WorkspaceId: apigen.ModelListTodosWorkspaceId(workspace)}})
	if err != nil {
		h.domainError(w, data, err)
		return
	}
	for _, todo := range result.Todos {
		data.Todos = append(data.Todos, todoView{ID: string(todo.Id), Title: string(todo.Title), Completed: bool(todo.Completed), Revision: int64(todo.Revision)})
	}
	h.render(w, http.StatusOK, data)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	workspace, title := strings.TrimSpace(r.FormValue("workspace")), strings.TrimSpace(r.FormValue("title"))
	data := h.validateCSRF(w, r, pageData{Title: "Todos", Workspace: workspace})
	if !data.CanMutate {
		return
	}
	if workspace == "" || title == "" {
		data.Error = "Enter a workspace ID and todo title."
		h.render(w, http.StatusBadRequest, data)
		return
	}
	if h.todos == nil {
		data.Error = "Todos are temporarily unavailable. Please try again later."
		h.render(w, http.StatusServiceUnavailable, data)
		return
	}
	_, err := h.todos.HandleTodosCreate(r.Context(), apigen.OpTodosCreateRequest{Body: apigen.ModelCreateTodo{WorkspaceId: apigen.ModelCreateTodoWorkspaceId(workspace), Title: apigen.ModelCreateTodoTitle(title)}})
	if err != nil {
		h.domainError(w, data, err)
		return
	}
	h.redirect(w, r, workspace, "Todo added.")
}

func (h *handler) edit(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspace")
	data, todo, ok := h.findTodo(w, r, workspace)
	if !ok {
		return
	}
	data.Editing = &todo
	h.mutationState(r, &data)
	h.render(w, http.StatusOK, data)
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	workspace, title := strings.TrimSpace(r.FormValue("workspace")), strings.TrimSpace(r.FormValue("title"))
	data := h.validateCSRF(w, r, pageData{Title: "Todos", Workspace: workspace})
	if !data.CanMutate {
		return
	}
	revision, err := strconv.ParseInt(r.FormValue("revision"), 10, 64)
	if workspace == "" || title == "" || err != nil || revision < 1 {
		data.Error = "This form is incomplete or invalid. Reload the todo and try again."
		h.render(w, http.StatusBadRequest, data)
		return
	}
	if h.todos == nil {
		data.Error = "Todos are temporarily unavailable. Please try again later."
		h.render(w, http.StatusServiceUnavailable, data)
		return
	}
	_, err = h.todos.HandleTodosUpdate(r.Context(), apigen.OpTodosUpdateRequest{Body: apigen.ModelUpdateTodo{WorkspaceId: apigen.ModelUpdateTodoWorkspaceId(workspace), TodoId: apigen.ModelUpdateTodoTodoId(r.PathValue("id")), Revision: apigen.ModelUpdateTodoRevision(revision), Title: apigen.ModelUpdateTodoTitle(title), Completed: r.FormValue("completed") == "true"}})
	if err != nil {
		h.domainError(w, data, err)
		return
	}
	h.redirect(w, r, workspace, "Todo updated.")
}

func (h *handler) findTodo(w http.ResponseWriter, r *http.Request, workspace string) (pageData, todoView, bool) {
	data := pageData{Title: "Todos", Workspace: workspace}
	if workspace == "" {
		data.Error = "Enter a workspace ID to open this todo."
		h.render(w, http.StatusBadRequest, data)
		return data, todoView{}, false
	}
	if h.todos == nil {
		data.Error = "Todos are temporarily unavailable. Please try again later."
		h.render(w, http.StatusServiceUnavailable, data)
		return data, todoView{}, false
	}
	result, err := h.todos.HandleTodosList(r.Context(), apigen.OpTodosListRequest{Body: apigen.ModelListTodos{WorkspaceId: apigen.ModelListTodosWorkspaceId(workspace)}})
	if err != nil {
		h.domainError(w, data, err)
		return data, todoView{}, false
	}
	for _, value := range result.Todos {
		if string(value.Id) == r.PathValue("id") {
			return data, todoView{ID: string(value.Id), Title: string(value.Title), Completed: bool(value.Completed), Revision: int64(value.Revision)}, true
		}
	}
	data.Error = "That todo is unavailable. Return to the list and choose an available todo."
	h.render(w, http.StatusNotFound, data)
	return data, todoView{}, false
}

func (h *handler) redirect(w http.ResponseWriter, r *http.Request, workspace, message string) {
	location := "/todos?" + url.Values{"workspace": {workspace}, "message": {message}}.Encode()
	if strings.Contains(r.Header.Get("HX-Request"), "true") {
		w.Header().Set("HX-Redirect", location)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (h *handler) mutationState(r *http.Request, data *pageData) {
	token, ok := h.resolveCSRF(r)
	h.mutationStateFromToken(data, token)
	if !ok {
		data.CanMutate = false
		data.CSRF = ""
		if data.Message == "" {
			data.Message = "Changes are unavailable until a protected session is ready."
		}
	}
}

func (h *handler) mutationStateFromToken(data *pageData, token string) {
	data.CSRF = token
	data.CanMutate = token != ""
}

func (h *handler) resolveCSRF(r *http.Request) (string, bool) {
	if h.csrfToken == nil {
		return "", false
	}
	token, ok := h.csrfToken(r)
	return token, ok && token != ""
}

func (h *handler) validateCSRF(w http.ResponseWriter, r *http.Request, data pageData) pageData {
	token, ok := h.resolveCSRF(r)
	if !ok {
		data.Message = "Changes are unavailable until a protected session is ready."
		data.Error = "This form could not be accepted. Sign in again and retry."
		h.render(w, http.StatusServiceUnavailable, data)
		return data
	}
	formTokens := r.PostForm["_csrf"]
	if len(formTokens) != 1 {
		data.Error = "This form has expired or is invalid. Reload the page and try again."
		h.render(w, http.StatusForbidden, data)
		return data
	}
	provided := formTokens[0]
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("X-CSRF-Token") != "" {
		provided = r.Header.Get("X-CSRF-Token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
		data.Error = "This form has expired or is invalid. Reload the page and try again."
		h.render(w, http.StatusForbidden, data)
		return data
	}
	h.mutationStateFromToken(&data, token)
	return data
}

func (h *handler) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			h.render(w, http.StatusRequestEntityTooLarge, pageData{Title: "Todos", Error: "This form is too large. Shorten the text and try again."})
			return false
		}
		h.render(w, http.StatusBadRequest, pageData{Title: "Todos", Error: "Invalid form submission."})
		return false
	}
	return true
}

func (h *handler) domainError(w http.ResponseWriter, data pageData, err error) {
	switch {
	case errors.Is(err, business.ErrRevisionConflict):
		data.Error = "This todo changed since you opened it. Reload the list and try again."
		data.Message = "Your changes were not saved."
		h.render(w, http.StatusConflict, data)
	case errors.Is(err, business.ErrWorkspaceUnavailable):
		data.Error = "This workspace or todo is unavailable. Check the workspace ID or return to the list."
		h.render(w, http.StatusForbidden, data)
	case errors.Is(err, business.ErrTodoUnavailable):
		data.Error = "This todo is unavailable. Return to the list and choose an available todo."
		h.render(w, http.StatusNotFound, data)
	case errors.Is(err, business.ErrInvalidInput):
		data.Error = "The todo details are invalid. Check the title and try again."
		h.render(w, http.StatusBadRequest, data)
	case errors.Is(err, business.ErrUnauthenticated):
		data.Error = "Sign in to view or change todos."
		h.render(w, http.StatusUnauthorized, data)
	default:
		data.Error = "AMOS could not complete that request. Please try again later."
		h.render(w, http.StatusServiceUnavailable, data)
	}
}

func (h *handler) render(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := page.Execute(w, data); err != nil {
		return
	}
}

func (h *handler) styles(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	h.serveAsset(w, "ui.css", "text/css; charset=utf-8")
}

func (h *handler) htmx(w http.ResponseWriter, _ *http.Request) {
	h.serveAsset(w, "htmx.min.js", "text/javascript; charset=utf-8")
}

func (h *handler) csrfScript(w http.ResponseWriter, _ *http.Request) {
	h.serveAsset(w, "csrf.js", "text/javascript; charset=utf-8")
}

func (h *handler) serveAsset(w http.ResponseWriter, name, contentType string) {
	w.Header().Set("Content-Type", contentType)
	content, err := stylesheet.ReadFile(name)
	if err != nil {
		http.Error(w, "Styles are temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(content)
}
