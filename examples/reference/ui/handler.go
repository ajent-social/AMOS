// Package ui provides the server-rendered reference landing and todo pages.
package ui

import (
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

//go:embed ui.css
var stylesheet embed.FS

// Todos is the public domain seam consumed by the reference pages. The request
// context is passed through unchanged so the host can attach the authenticated
// principal before this handler runs.
type Todos interface {
	apigen.OpTodosCreateHandler
	apigen.OpTodosListHandler
	apigen.OpTodosUpdateHandler
}

// NewHandler returns the reference landing and todo routes. The host remains
// responsible for authentication, workspace selection, and mounting the routes.
func NewHandler(todos Todos) http.Handler {
	h := &handler{todos: todos}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.home)
	mux.HandleFunc("GET /reference/ui.css", h.styles)
	mux.HandleFunc("GET /todos", h.list)
	mux.HandleFunc("POST /todos", h.create)
	mux.HandleFunc("GET /todos/{id}/edit", h.edit)
	mux.HandleFunc("POST /todos/{id}/edit", h.update)
	return mux
}

type handler struct{ todos Todos }

type pageData struct {
	Title, Workspace, Message, Error, TodoID string
	Todos                                    []todoView
	Editing                                  *todoView
}

type todoView struct {
	ID, Title string
	Completed bool
	Revision  int64
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en" style="color-scheme: light dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}} · AMOS reference</title><link rel="stylesheet" href="/reference/ui.css"><script src="https://unpkg.com/htmx.org@2.0.4" defer></script></head>
<body><a class="skip" href="#main">Skip to content</a><header><a href="/">AMOS reference</a><nav><a href="/todos?workspace={{urlquery .Workspace}}">Todos</a></nav></header><main id="main">
{{if .Message}}<p role="status" class="notice">{{.Message}}</p>{{end}}{{if .Error}}<p role="alert" class="error">{{.Error}}</p>{{end}}
{{if eq .Title "Your work, in one place"}}<section class="hero"><p class="eyebrow">A small business workspace</p><h1>Your work, in one place</h1><p>Keep a shared list of the next things to do.</p><form action="/todos" method="get"><label for="home-workspace">Workspace ID</label><div class="row"><input id="home-workspace" name="workspace" value="{{.Workspace}}" required autocomplete="off"><button type="submit">Open todos</button></div></form></section>
{{else}}<section><p class="eyebrow">Reference workspace</p><h1>Todos</h1><form action="/todos" method="get"><label for="workspace">Workspace ID</label><div class="row"><input id="workspace" name="workspace" value="{{.Workspace}}" required autocomplete="off"><button type="submit">Load workspace</button></div></form>
{{if .Workspace}}<section class="card"><h2>{{if .Editing}}Edit todo{{else}}Add a todo{{end}}</h2>{{if .Editing}}<form action="/todos/{{.Editing.ID}}/edit" method="post" hx-boost="true"><input type="hidden" name="workspace" value="{{.Workspace}}"><input type="hidden" name="revision" value="{{.Editing.Revision}}"><label for="title">Title</label><input id="title" name="title" value="{{.Editing.Title}}" required maxlength="200"><label><input type="checkbox" name="completed" value="true" {{if .Editing.Completed}}checked{{end}}> Completed</label><button type="submit">Save changes</button><a href="/todos?workspace={{urlquery .Workspace}}">Cancel</a></form>{{else}}<form action="/todos" method="post" hx-boost="true"><input type="hidden" name="workspace" value="{{.Workspace}}"><label for="title">New todo</label><div class="row"><input id="title" name="title" required maxlength="200" autocomplete="off"><button type="submit">Add todo</button></div></form>{{end}}</section>
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form submission.", http.StatusBadRequest)
		return
	}
	workspace, title := strings.TrimSpace(r.FormValue("workspace")), strings.TrimSpace(r.FormValue("title"))
	if workspace == "" || title == "" {
		h.render(w, http.StatusBadRequest, pageData{Title: "Todos", Workspace: workspace, Error: "Enter a workspace ID and todo title."})
		return
	}
	if h.todos == nil {
		h.render(w, http.StatusServiceUnavailable, pageData{Title: "Todos", Workspace: workspace, Error: "Todos are temporarily unavailable. Please try again later."})
		return
	}
	_, err := h.todos.HandleTodosCreate(r.Context(), apigen.OpTodosCreateRequest{Body: apigen.ModelCreateTodo{WorkspaceId: apigen.ModelCreateTodoWorkspaceId(workspace), Title: apigen.ModelCreateTodoTitle(title)}})
	if err != nil {
		h.domainError(w, pageData{Title: "Todos", Workspace: workspace}, err)
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
	h.render(w, http.StatusOK, data)
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form submission.", http.StatusBadRequest)
		return
	}
	workspace, title := strings.TrimSpace(r.FormValue("workspace")), strings.TrimSpace(r.FormValue("title"))
	revision, err := strconv.ParseInt(r.FormValue("revision"), 10, 64)
	if workspace == "" || title == "" || err != nil || revision < 1 {
		h.render(w, http.StatusBadRequest, pageData{Title: "Todos", Workspace: workspace, Error: "This form is incomplete or invalid. Reload the todo and try again."})
		return
	}
	if h.todos == nil {
		h.render(w, http.StatusServiceUnavailable, pageData{Title: "Todos", Workspace: workspace, Error: "Todos are temporarily unavailable. Please try again later."})
		return
	}
	_, err = h.todos.HandleTodosUpdate(r.Context(), apigen.OpTodosUpdateRequest{Body: apigen.ModelUpdateTodo{WorkspaceId: apigen.ModelUpdateTodoWorkspaceId(workspace), TodoId: apigen.ModelUpdateTodoTodoId(r.PathValue("id")), Revision: apigen.ModelUpdateTodoRevision(revision), Title: apigen.ModelUpdateTodoTitle(title), Completed: r.FormValue("completed") == "true"}})
	if err != nil {
		h.domainError(w, pageData{Title: "Todos", Workspace: workspace}, err)
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
	w.Header().Set("Cache-Control", "public, max-age=3600")
	content, err := stylesheet.ReadFile("ui.css")
	if err != nil {
		http.Error(w, "Styles are temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(content)
}
