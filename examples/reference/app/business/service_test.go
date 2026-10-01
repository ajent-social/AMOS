package business

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/reference/app/business/apigen"
	identitysession "github.com/ajent-social/amos/identity/session"
	identitystore "github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/codegen/gotypes"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestTodoCRUDSurvivesRestartAndEnforcesWorkspaceBoundary(t *testing.T) {
	h := newBusinessHarness(t)
	auth := newSessionService(t, h.db, h.scope, h.environment)
	svc, err := New(h.db)
	if err != nil {
		t.Fatal(err)
	}

	var created apigen.OpTodosCreateResponse
	err = callAs(t, auth, h.tokens[h.ownerA], http.MethodPost, func(ctx context.Context) error {
		var e error
		created, e = svc.HandleTodosCreate(ctx, apigen.OpTodosCreateRequest{Body: apigen.ModelCreateTodo{
			WorkspaceId: apigen.ModelCreateTodoWorkspaceId(h.workspaceA.String()),
			Title:       apigen.ModelCreateTodoTitle("shared reference task"),
		}})
		return e
	})
	if err != nil || created.Revision != 1 || string(created.Title) != "shared reference task" {
		t.Fatalf("create = %#v, %v", created, err)
	}

	var foreignTodo apigen.OpTodosCreateResponse
	err = callAs(t, auth, h.tokens[h.ownerB], http.MethodPost, func(ctx context.Context) error {
		var e error
		foreignTodo, e = svc.HandleTodosCreate(ctx, apigen.OpTodosCreateRequest{Body: apigen.ModelCreateTodo{
			WorkspaceId: apigen.ModelCreateTodoWorkspaceId(h.workspaceB.String()),
			Title:       apigen.ModelCreateTodoTitle("private to another workspace"),
		}})
		return e
	})
	if err != nil {
		t.Fatalf("create foreign-workspace fixture: %v", err)
	}

	// A second current member of the same workspace can read and update the
	// shared resource; creator identity is audit data, not a tenant boundary.
	var listed apigen.OpTodosListResponse
	err = callAs(t, auth, h.tokens[h.memberA], http.MethodPost, func(ctx context.Context) error {
		var e error
		listed, e = svc.HandleTodosList(ctx, apigen.OpTodosListRequest{Body: apigen.ModelListTodos{
			WorkspaceId: apigen.ModelListTodosWorkspaceId(h.workspaceA.String()),
		}})
		return e
	})
	if err != nil || len(listed.Todos) != 1 || string(listed.Todos[0].Id) != string(created.Id) {
		t.Fatalf("list = %#v, %v", listed, err)
	}
	if string(listed.Todos[0].Id) == string(foreignTodo.Id) {
		t.Fatal("list leaked a resource from another workspace")
	}

	update := apigen.OpTodosUpdateRequest{Body: apigen.ModelUpdateTodo{
		WorkspaceId: apigen.ModelUpdateTodoWorkspaceId(h.workspaceA.String()),
		TodoId:      apigen.ModelUpdateTodoTodoId(string(created.Id)),
		Revision:    1,
		Title:       apigen.ModelUpdateTodoTitle("updated by another member"),
		Completed:   true,
	}}
	var updated apigen.OpTodosUpdateResponse
	err = callAs(t, auth, h.tokens[h.memberA], http.MethodPut, func(ctx context.Context) error {
		var e error
		updated, e = svc.HandleTodosUpdate(ctx, update)
		return e
	})
	if err != nil || updated.Revision != 2 || !updated.Completed {
		t.Fatalf("update = %#v, %v", updated, err)
	}

	stale := update
	stale.Body.Title = apigen.ModelUpdateTodoTitle("stale overwrite")
	err = callAs(t, auth, h.tokens[h.ownerA], http.MethodPut, func(ctx context.Context) error {
		_, e := svc.HandleTodosUpdate(ctx, stale)
		return e
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error = %v, want revision conflict", err)
	}
	foreignUpdateByID := apigen.OpTodosUpdateRequest{Body: apigen.ModelUpdateTodo{
		WorkspaceId: apigen.ModelUpdateTodoWorkspaceId(h.workspaceA.String()),
		TodoId:      apigen.ModelUpdateTodoTodoId(string(foreignTodo.Id)),
		Revision:    1,
		Title:       apigen.ModelUpdateTodoTitle("cross-workspace overwrite"),
	}}
	err = callAs(t, auth, h.tokens[h.ownerA], http.MethodPut, func(ctx context.Context) error {
		_, e := svc.HandleTodosUpdate(ctx, foreignUpdateByID)
		return e
	})
	if !errors.Is(err, ErrTodoUnavailable) {
		t.Fatalf("foreign todo update under an authorized workspace = %v, want unavailable", err)
	}
	err = callAs(t, auth, h.tokens[h.ownerA], http.MethodDelete, func(ctx context.Context) error {
		_, e := svc.HandleTodosDelete(ctx, apigen.OpTodosDeleteRequest{Body: apigen.ModelDeleteTodo{
			WorkspaceId: apigen.ModelDeleteTodoWorkspaceId(h.workspaceA.String()),
			TodoId:      apigen.ModelDeleteTodoTodoId(string(foreignTodo.Id)),
			Revision:    1,
		}})
		return e
	})
	if !errors.Is(err, ErrTodoUnavailable) {
		t.Fatalf("foreign todo delete under an authorized workspace = %v, want unavailable", err)
	}

	// Simulate a process restart by closing and reopening the storage pool while
	// preserving the isolated database schema and its committed rows.
	dsn, err := scopedDatabaseURL(t, h.schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.db.Close(); err != nil {
		t.Fatal(err)
	}
	h.db, err = storage.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("reopen database after restart: %v", err)
	}
	newAuth := newSessionService(t, h.db, h.scope, h.environment)
	svc, err = New(h.db)
	if err != nil {
		t.Fatal(err)
	}
	listed = apigen.OpTodosListResponse{}
	err = callAs(t, newAuth, h.tokens[h.memberA], http.MethodPost, func(ctx context.Context) error {
		var e error
		listed, e = svc.HandleTodosList(ctx, apigen.OpTodosListRequest{Body: apigen.ModelListTodos{
			WorkspaceId: apigen.ModelListTodosWorkspaceId(h.workspaceA.String()),
		}})
		return e
	})
	if err != nil || len(listed.Todos) != 1 || listed.Todos[0].Revision != 2 || !listed.Todos[0].Completed {
		t.Fatalf("persisted list after restart = %#v, %v", listed, err)
	}

	// A valid workspace identifier from another organization is only a selector;
	// membership validation denies it before any resource query can succeed.
	err = callAs(t, newAuth, h.tokens[h.ownerA], http.MethodPost, func(ctx context.Context) error {
		_, e := svc.HandleTodosList(ctx, apigen.OpTodosListRequest{Body: apigen.ModelListTodos{
			WorkspaceId: apigen.ModelListTodosWorkspaceId(h.workspaceB.String()),
		}})
		return e
	})
	if !errors.Is(err, ErrWorkspaceUnavailable) {
		t.Fatalf("foreign workspace list error = %v, want unavailable", err)
	}
	foreignUpdate := update
	foreignUpdate.Body.WorkspaceId = apigen.ModelUpdateTodoWorkspaceId(h.workspaceB.String())
	err = callAs(t, newAuth, h.tokens[h.ownerA], http.MethodPut, func(ctx context.Context) error {
		_, e := svc.HandleTodosUpdate(ctx, foreignUpdate)
		return e
	})
	if !errors.Is(err, ErrWorkspaceUnavailable) {
		t.Fatalf("foreign workspace update error = %v, want unavailable", err)
	}

	// The selector alone cannot create authority when trusted identity
	// middleware has not attached a principal.
	_, err = svc.HandleTodosList(context.Background(), apigen.OpTodosListRequest{Body: apigen.ModelListTodos{
		WorkspaceId: apigen.ModelListTodosWorkspaceId(h.workspaceA.String()),
	}})
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unauthenticated list error = %v", err)
	}

	deleteRequest := apigen.OpTodosDeleteRequest{Body: apigen.ModelDeleteTodo{
		WorkspaceId: apigen.ModelDeleteTodoWorkspaceId(h.workspaceA.String()),
		TodoId:      apigen.ModelDeleteTodoTodoId(string(created.Id)),
		Revision:    2,
	}}
	err = callAs(t, newAuth, h.tokens[h.ownerA], http.MethodDelete, func(ctx context.Context) error {
		_, e := svc.HandleTodosDelete(ctx, deleteRequest)
		return e
	})
	if err != nil {
		t.Fatalf("delete = %v", err)
	}
	listed = apigen.OpTodosListResponse{}
	err = callAs(t, newAuth, h.tokens[h.ownerA], http.MethodPost, func(ctx context.Context) error {
		var e error
		listed, e = svc.HandleTodosList(ctx, apigen.OpTodosListRequest{Body: apigen.ModelListTodos{
			WorkspaceId: apigen.ModelListTodosWorkspaceId(h.workspaceA.String()),
		}})
		return e
	})
	if err != nil || len(listed.Todos) != 0 {
		t.Fatalf("list after delete = %#v, %v", listed, err)
	}
}

func TestGeneratedBusinessAPIIsCurrent(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "api", "business.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := gotypes.Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join("apigen", "api_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != string(committed) {
		t.Fatal("generated Go API is stale; regenerate from business.openapi.yaml")
	}
}

type businessHarness struct {
	db          *storage.DB
	schema      string
	scope       workspacestore.Scope
	environment uuid.UUID
	ownerA      uuid.UUID
	memberA     uuid.UUID
	ownerB      uuid.UUID
	workspaceA  uuid.UUID
	workspaceB  uuid.UUID
	tokens      map[uuid.UUID]string
}

func newBusinessHarness(t *testing.T) *businessHarness {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := scopedDatabaseURL(t, schema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	h := &businessHarness{db: db, schema: schema}
	t.Cleanup(func() {
		if err := h.db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL := readFile(t, filepath.Join("..", "..", "..", "..", "migrations", "fragments", "identity.sql"))
	workspaceSQL := readFile(t, filepath.Join("..", "..", "..", "..", "migrations", "fragments", "workspace.sql"))
	todoSQL := readFile(t, filepath.Join("..", "..", "migrations", "todos.sql"))
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: string(workspaceSQL)}}},
		migrations.Fragment{Namespace: "reference", Migrations: []migrations.Migration{{Sequence: 3, Name: "todos_base", SQL: string(todoSQL)}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(context.Background(), db, registry); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	h.scope = workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}
	h.environment = newID(t)
	h.ownerA = newID(t)
	h.memberA = newID(t)
	h.ownerB = newID(t)
	h.tokens = make(map[uuid.UUID]string)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		for _, person := range []uuid.UUID{h.ownerA, h.memberA, h.ownerB} {
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, h.scope.InstallationID, h.scope.ApplicationID); err != nil {
				return err
			}
		}
		workspaces, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		orgA, _, err := workspaces.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{ID: newID(t), OwnerMembershipID: newID(t), Scope: h.scope, OwnerPersonID: h.ownerA})
		if err != nil {
			return err
		}
		h.workspaceA = orgA.ID
		orgB, _, err := workspaces.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{ID: newID(t), OwnerMembershipID: newID(t), Scope: h.scope, OwnerPersonID: h.ownerB})
		if err != nil {
			return err
		}
		h.workspaceB = orgB.ID
		_, err = workspaces.AddMembership(context.Background(), workspacestore.AddMembershipInput{ID: newID(t), Scope: h.scope, WorkspaceID: h.workspaceA, PersonID: h.memberA, Role: workspacestore.RoleMember, RoleVersion: 1})
		return err
	}); err != nil {
		t.Fatalf("seed workspaces and memberships: %v", err)
	}
	for _, person := range []uuid.UUID{h.ownerA, h.memberA, h.ownerB} {
		h.tokens[person] = createSessionToken(t, db, h.scope, h.environment, person)
	}
	return h
}

func newSessionService(t *testing.T, db *storage.DB, scope workspacestore.Scope, environment uuid.UUID) *identitysession.Service {
	t.Helper()
	service, err := identitysession.New(db, identitysession.Config{
		InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EnvironmentID: environment,
		AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func createSessionToken(t *testing.T, db *storage.DB, scope workspacestore.Scope, environment, person uuid.UUID) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	now := time.Now().UTC().Add(-time.Second)
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := identitystore.New(tx)
		if err != nil {
			return err
		}
		return store.CreateSession(context.Background(), identitystore.Session{
			ID: newID(t), PersonID: person, InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID,
			EnvironmentID: environment, TokenDigest: digest[:], AuthenticationMethod: "email_password",
			AuthenticatedAt: now, ExpiresAt: now.Add(10 * time.Hour),
		})
	})
	if err != nil {
		t.Fatalf("seed verified session digest: %v", err)
	}
	return token
}

func callAs(t *testing.T, auth *identitysession.Service, token, method string, call func(context.Context) error) error {
	t.Helper()
	var callErr error
	request := httptest.NewRequest(method, "https://app.example.test/reference", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		request.Header.Set("Origin", "https://app.example.test")
		request.Header.Set("X-CSRF-Token", csrfForToken(t, token))
	}
	response := httptest.NewRecorder()
	auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callErr = call(r.Context())
	})).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("verified-session middleware status=%d body=%q", response.Code, response.Body.String())
	}
	return callErr
}

func csrfForToken(t *testing.T, token string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("amos-session-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func scopedDatabaseURL(t *testing.T, schema string) (string, error) {
	t.Helper()
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPersonalWorkspaceOnlyAllowsCurrentOwner(t *testing.T) {
	h := newBusinessHarness(t)
	auth := newSessionService(t, h.db, h.scope, h.environment)
	svc, err := New(h.db)
	if err != nil {
		t.Fatal(err)
	}
	var workspaceID uuid.UUID
	if err = h.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		ws, e := store.CreatePersonalWorkspace(context.Background(), workspacestore.CreatePersonalInput{ID: newID(t), Scope: h.scope, OwnerPersonID: h.ownerA})
		workspaceID = ws.ID
		return e
	}); err != nil {
		t.Fatal(err)
	}
	for _, person := range []uuid.UUID{h.ownerA, h.memberA, h.ownerB} {
		err = callAs(t, auth, h.tokens[person], http.MethodPost, func(ctx context.Context) error {
			_, e := svc.HandleTodosCreate(ctx, apigen.OpTodosCreateRequest{Body: apigen.ModelCreateTodo{WorkspaceId: apigen.ModelCreateTodoWorkspaceId(workspaceID.String()), Title: "personal task"}})
			return e
		})
		if person == h.ownerA {
			if err != nil {
				t.Fatalf("owner: %v", err)
			}
		} else if !errors.Is(err, ErrWorkspaceUnavailable) {
			t.Fatalf("nonowner: %v", err)
		}
	}
}
