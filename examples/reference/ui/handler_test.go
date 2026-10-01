package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ajent-social/amos/examples/reference/app/business"
	"github.com/ajent-social/amos/examples/reference/app/business/apigen"
)

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
	NewHandler(domain).ServeHTTP(response, request)

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
	handler := NewHandler(domain)
	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("workspace=ws&title=%20%20"))
	invalidRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "Enter a workspace ID and todo title") {
		t.Fatalf("invalid form response = %d %q", invalid.Code, invalid.Body.String())
	}

	valid := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("workspace=ws&title=Plan"))
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
	request := httptest.NewRequest(http.MethodPost, "/todos/todo-1/edit", strings.NewReader("workspace=ws&title=New&revision=1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	NewHandler(domain).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "changed since you opened it") {
		t.Fatalf("unexpected stale failure response: %d %q", response.Code, response.Body.String())
	}
}

func TestUnavailableWorkspaceDoesNotRevealTenantData(t *testing.T) {
	domain := &fakeTodos{listErr: business.ErrWorkspaceUnavailable}
	request := httptest.NewRequest(http.MethodGet, "/todos?workspace=private-id", nil)
	response := httptest.NewRecorder()
	NewHandler(domain).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "workspace or todo is unavailable") {
		t.Fatalf("unexpected unavailable response: %d %q", response.Code, response.Body.String())
	}
}
