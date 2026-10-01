package ui

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ajent-social/amos/examples/reference/app/business"
	"github.com/ajent-social/amos/examples/reference/app/business/apigen"
)

func handlerWithTestCSRF(todos Todos) http.Handler {
	return NewHandler(todos, Options{CSRFToken: func(*http.Request) (string, bool) { return "csrf-test", true }})
}

type fakeTodos struct {
	list      apigen.OpTodosListResponse
	listErr   error
	createErr error
	updateErr error
	created   apigen.OpTodosCreateRequest
	updated   apigen.OpTodosUpdateRequest
}

func (f *fakeTodos) HandleTodosList(context.Context, apigen.OpTodosListRequest) (apigen.OpTodosListResponse, error) {
	return f.list, f.listErr
}
func (f *fakeTodos) HandleTodosCreate(_ context.Context, req apigen.OpTodosCreateRequest) (apigen.OpTodosCreateResponse, error) {
	f.created = req
	return apigen.OpTodosCreateResponse{}, f.createErr
}
func (f *fakeTodos) HandleTodosUpdate(_ context.Context, req apigen.OpTodosUpdateRequest) (apigen.OpTodosUpdateResponse, error) {
	f.updated = req
	return apigen.OpTodosUpdateResponse{}, f.updateErr
}

func TestListRendersUntrustedTodoAsText(t *testing.T) {
	domain := &fakeTodos{list: apigen.OpTodosListResponse{Todos: []apigen.ModelTodo{{
		Id: "todo-1", Title: apigen.ModelTodoTitle(`<img src=x onerror=alert(1)>`), Revision: 1,
	}}}}
	request := httptest.NewRequest(http.MethodGet, "/todos?workspace=workspace-1", nil)
	response := httptest.NewRecorder()
	handlerWithTestCSRF(domain).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), `<img src=x onerror=alert(1)>`) {
		t.Fatal("untrusted todo title was emitted as HTML")
	}
	if !strings.Contains(response.Body.String(), `&lt;img src=x onerror=alert(1)&gt;`) {
		t.Fatal("escaped todo title was not visible as text")
	}
}

func TestCreateRequiresTitleAndUsesPostRedirectGet(t *testing.T) {
	domain := &fakeTodos{}
	handler := handlerWithTestCSRF(domain)
	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("_csrf=csrf-test&workspace=ws&title=%20%20"))
	invalidRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "Enter a workspace ID and todo title") {
		t.Fatalf("invalid form response = %d %q", invalid.Code, invalid.Body.String())
	}

	valid := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("_csrf=csrf-test&workspace=ws&title=Plan"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(valid, req)
	if valid.Code != http.StatusSeeOther || valid.Header().Get("Location") != "/todos?message=Todo+added.&workspace=ws" {
		t.Fatalf("create response = %d, location %q", valid.Code, valid.Header().Get("Location"))
	}
	if domain.created.Body.Title != "Plan" || domain.created.Body.WorkspaceId != "ws" {
		t.Fatalf("unexpected domain request: %#v", domain.created)
	}
}

func TestStaleEditIsVisibleAndSafe(t *testing.T) {
	domain := &fakeTodos{list: apigen.OpTodosListResponse{Todos: []apigen.ModelTodo{{Id: "todo-1", Title: "Plan", Revision: 1}}}, updateErr: business.ErrRevisionConflict}
	// The browser-facing message never contains arbitrary provider diagnostics.
	request := httptest.NewRequest(http.MethodPost, "/todos/todo-1/edit", strings.NewReader("_csrf=csrf-test&workspace=ws&title=New&revision=1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handlerWithTestCSRF(domain).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "changed since you opened it") {
		t.Fatalf("unexpected stale failure response: %d %q", response.Code, response.Body.String())
	}
}

func TestUnavailableWorkspaceDoesNotRevealTenantData(t *testing.T) {
	domain := &fakeTodos{listErr: business.ErrWorkspaceUnavailable}
	request := httptest.NewRequest(http.MethodGet, "/todos?workspace=private-id", nil)
	response := httptest.NewRecorder()
	handlerWithTestCSRF(domain).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "workspace or todo is unavailable") {
		t.Fatalf("unexpected unavailable response: %d %q", response.Code, response.Body.String())
	}
}

func TestMissingCSRFResolverHidesMutationFormsAndFailsClosed(t *testing.T) {
	domain := &fakeTodos{}
	response := httptest.NewRecorder()
	NewHandler(domain).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/todos?workspace=ws", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `method="post"`) {
		t.Fatalf("unexpected page without CSRF resolver: %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "protected session is ready") {
		t.Fatal("missing resolver state is not visible")
	}

	request := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("_csrf=csrf-test&workspace=ws&title=Plan"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	NewHandler(domain).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || domain.created.Body.Title != "" {
		t.Fatalf("mutation did not fail closed: status %d, request %#v", response.Code, domain.created)
	}
}

func TestCSRFTokenIsEscapedAndSecurityHeadersArePrivate(t *testing.T) {
	token := `csrf<&"value`
	domain := &fakeTodos{}
	server := NewHandler(domain, Options{CSRFToken: func(*http.Request) (string, bool) { return token, true }})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/todos?workspace=ws", nil))
	if !strings.Contains(response.Body.String(), html.EscapeString(token)) || strings.Contains(response.Body.String(), token) {
		t.Fatal("CSRF token was not escaped in the rendered form")
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestOversizedFormIsRejectedBeforeDomainMutation(t *testing.T) {
	domain := &fakeTodos{}
	body := "_csrf=csrf-test&workspace=ws&title=" + strings.Repeat("x", 16<<10)
	request := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handlerWithTestCSRF(domain).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || domain.created.Body.Title != "" {
		t.Fatalf("oversized form was not rejected before mutation: status %d", response.Code)
	}
}

func TestInvalidAndMissingCSRFValuesDoNotMutate(t *testing.T) {
	for _, form := range []string{
		"workspace=ws&title=Plan",
		"_csrf=wrong&workspace=ws&title=Plan",
	} {
		domain := &fakeTodos{}
		request := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(form))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handlerWithTestCSRF(domain).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || domain.created.Body.Title != "" {
			t.Fatalf("CSRF failure mutated data: status %d", response.Code)
		}
	}
}

func TestHTMXUsesSessionCSRFHeader(t *testing.T) {
	domain := &fakeTodos{}
	request := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("workspace=ws&title=Plan"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("X-CSRF-Token", "csrf-test")
	response := httptest.NewRecorder()
	handlerWithTestCSRF(domain).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("HX-Redirect") == "" {
		t.Fatalf("HTMX request was not redirected: %d, %q", response.Code, response.Header().Get("HX-Redirect"))
	}
	if domain.created.Body.Title != "Plan" {
		t.Fatalf("valid HTMX CSRF header did not reach the domain seam: %#v", domain.created)
	}
}
