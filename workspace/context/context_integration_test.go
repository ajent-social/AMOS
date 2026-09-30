package context

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestT4_4_SelectionSwitchRevocationAndForgedIdentityHeaders(t *testing.T) {
	db, cfg, owner, member := newContextDB(t)
	personal := createPersonal(t, db, cfg, owner)
	org := createOrganization(t, db, workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, owner)
	addMember(t, db, cfg, org.ID, member)
	foreignOrg := createOrganization(t, db, workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, newPerson(t, db, workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}))
	foreignScope := workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}
	foreignPerson := newPerson(t, db, foreignScope)
	foreignWorkspace := createOrganization(t, db, foreignScope, foreignPerson)

	var reached atomic.Int32
	var lastWorkspace atomic.Value
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selection, ok := FromContext(r.Context())
		if !ok {
			t.Error("protected handler received no workspace selection")
			http.Error(w, "missing selection", http.StatusInternalServerError)
			return
		}
		if selection.Workspace.Kind == workspacestore.KindOrganization &&
			(selection.Membership == nil || selection.MembershipEpoch != selection.Membership.Epoch || len(selection.Permissions) == 0) {
			t.Error("organization selection omitted current membership, role permissions, or epoch")
		}
		lastWorkspace.Store(selection.Workspace.ID)
		reached.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	resolver, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sessions := newSessionService(t, db, cfg)
	handler := sessions.Middleware(resolver.Middleware(protected))
	ownerCookie := insertSession(t, db, cfg, owner)
	memberCookie := insertSession(t, db, cfg, member)
	memberHintResponse := serveSelection(handler, memberCookie, org.ID, nil)
	if memberHintResponse.Code != http.StatusNoContent {
		t.Fatalf("member organization request status=%d body=%s", memberHintResponse.Code, memberHintResponse.Body.String())
	}
	var memberHint *http.Cookie
	for _, cookie := range memberHintResponse.Result().Cookies() {
		if cookie.Name == workspaceHintCookie {
			memberHint = cookie
		}
	}
	if memberHint == nil {
		t.Fatal("authorized organization selection did not persist a workspace hint")
	}

	for _, tc := range []struct {
		name      string
		cookie    string
		workspace uuid.UUID
	}{
		{name: "personal workspace", cookie: ownerCookie, workspace: personal.ID},
		{name: "organization switch", cookie: ownerCookie, workspace: org.ID},
		{name: "current member access", cookie: memberCookie, workspace: org.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := serveSelection(handler, tc.cookie, tc.workspace, nil)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s, want 204", recorder.Code, recorder.Body.String())
			}
			got, ok := lastWorkspace.Load().(uuid.UUID)
			if !ok || got != tc.workspace {
				t.Fatalf("handler workspace=%v, want %s", got, tc.workspace)
			}
		})
	}

	before := reached.Load()
	unknown := serveSelection(handler, memberCookie, newID(t), nil)
	foreign := serveSelection(handler, memberCookie, foreignWorkspace.ID, nil)
	unauthorized := serveSelection(handler, memberCookie, foreignOrg.ID, nil)
	for name, recorder := range map[string]*httptest.ResponseRecorder{"unknown": unknown, "cross-scope": foreign, "unmembered organization": unauthorized} {
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"workspace.denied"`) {
			t.Errorf("%s result=%d %s, want generic workspace.denied", name, recorder.Code, recorder.Body.String())
		}
	}
	if unknown.Body.String() != unauthorized.Body.String() {
		t.Errorf("unknown and unauthorized selections disclose different responses: %q != %q", unknown.Body.String(), unauthorized.Body.String())
	}
	if reached.Load() != before {
		t.Fatal("unauthorized workspace reached protected handler")
	}

	forged := serveSelection(handler, ownerCookie, org.ID, map[string]string{"X-Person-ID": owner.String()})
	if forged.Code != http.StatusBadRequest || reached.Load() != before {
		t.Fatalf("forged identity header status=%d handler calls=%d, want 400 and no handler", forged.Code, reached.Load()-before)
	}

	// The browser cookie stores only the selected workspace ID. A request
	// selector can switch it, and the stored ID is validated again on use.
	hintSwitch := serveSelectionWithHint(handler, ownerCookie, personal.ID, org.ID)
	if hintSwitch.Code != http.StatusNoContent {
		t.Fatalf("workspace switch from stored hint status=%d body=%s, want 204", hintSwitch.Code, hintSwitch.Body.String())
	}
	var storedHint *http.Cookie
	for _, cookie := range hintSwitch.Result().Cookies() {
		if cookie.Name == workspaceHintCookie {
			storedHint = cookie
		}
	}
	if storedHint == nil || storedHint.Value != org.ID.String() || !storedHint.HttpOnly || !storedHint.Secure || storedHint.SameSite != http.SameSiteLaxMode {
		t.Fatalf("workspace hint cookie=%#v, want only authorized ID with HttpOnly/Secure/SameSite=Lax", storedHint)
	}
	cookieOnly := serveSelectionWithCookie(handler, ownerCookie, storedHint)
	if cookieOnly.Code != http.StatusNoContent {
		t.Fatalf("validated browser hint status=%d body=%s, want 204", cookieOnly.Code, cookieOnly.Body.String())
	}
	malformedHintOverride := serveSelectionWithMalformedHint(handler, ownerCookie, org.ID)
	if malformedHintOverride.Code != http.StatusNoContent {
		t.Fatalf("explicit selection could not replace malformed hint status=%d body=%s", malformedHintOverride.Code, malformedHintOverride.Body.String())
	}

	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		_, err = st.UpdateMembership(context.Background(), workspacestore.UpdateMembershipInput{
			Scope:       workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID},
			WorkspaceID: org.ID, PersonID: member, Role: workspacestore.RoleMember,
			RoleVersion: 1, State: workspacestore.MembershipSuspended,
		})
		return err
	}); err != nil {
		t.Fatalf("suspend current organization membership: %v", err)
	}
	before = reached.Load()
	stale := serveSelectionWithCookie(handler, memberCookie, memberHint)
	if stale.Code != http.StatusForbidden || !strings.Contains(stale.Body.String(), `"code":"workspace.denied"`) || reached.Load() != before {
		t.Fatalf("stale membership status=%d calls=%d body=%s", stale.Code, reached.Load()-before, stale.Body.String())
	}
}

func TestT4_4_DatabaseOutageReturnsUnavailableBeforeHandler(t *testing.T) {
	db, cfg, owner, _ := newContextDB(t)
	personal := createPersonal(t, db, cfg, owner)
	cookie := insertSession(t, db, cfg, owner)
	var called atomic.Bool
	protected := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) })
	resolver, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	handler := newSessionService(t, db, cfg).Middleware(resolver.Middleware(protected))
	if err := db.Close(); err != nil {
		t.Fatalf("close test database: %v", err)
	}
	recorder := serveSelection(handler, cookie, personal.ID, nil)
	if recorder.Code != http.StatusServiceUnavailable || called.Load() {
		t.Fatalf("outage status=%d handlerCalled=%v body=%s, want 503 and no handler", recorder.Code, called.Load(), recorder.Body.String())
	}
}

func TestT4_4_EnvironmentAndSecurityEpochAreCurrent(t *testing.T) {
	db, cfg, owner, _ := newContextDB(t)
	personal := createPersonal(t, db, cfg, owner)
	cookie := insertSession(t, db, cfg, owner)
	var called atomic.Bool
	protected := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) })
	wrongEnv := cfg
	wrongEnv.EnvironmentID = newID(t)
	resolver, err := New(db, wrongEnv)
	if err != nil {
		t.Fatal(err)
	}
	handler := newSessionService(t, db, cfg).Middleware(resolver.Middleware(protected))
	recorder := serveSelection(handler, cookie, personal.ID, nil)
	if recorder.Code != http.StatusForbidden || called.Load() {
		t.Fatalf("wrong environment status=%d called=%v, want denied before handler", recorder.Code, called.Load())
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, owner)
		return err
	}); err != nil {
		t.Fatalf("advance current account security epoch: %v", err)
	}
	currentResolver, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	currentHandler := newSessionService(t, db, cfg).Middleware(currentResolver.Middleware(protected))
	recorder = serveSelection(currentHandler, cookie, personal.ID, nil)
	if recorder.Code != http.StatusUnauthorized || called.Load() {
		t.Fatalf("stale account epoch status=%d called=%v, want unauthenticated before handler", recorder.Code, called.Load())
	}
}

func serveSelection(handler http.Handler, cookie string, workspace uuid.UUID, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3000/protected", nil)
	req.AddCookie(&http.Cookie{Name: "amos_dev_session", Value: cookie})
	req.Header.Set("X-Workspace-ID", workspace.String())
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func serveSelectionWithHint(handler http.Handler, cookie string, hint, header uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3000/protected", nil)
	req.AddCookie(&http.Cookie{Name: "amos_dev_session", Value: cookie})
	req.Header.Set("X-Workspace-ID", header.String())
	req = req.WithContext(WithWorkspaceHint(req.Context(), hint))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func serveSelectionWithCookie(handler http.Handler, sessionCookie string, hint *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3000/protected", nil)
	req.AddCookie(&http.Cookie{Name: "amos_dev_session", Value: sessionCookie})
	req.AddCookie(hint)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func serveSelectionWithMalformedHint(handler http.Handler, sessionCookie string, workspace uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3000/protected", nil)
	req.AddCookie(&http.Cookie{Name: "amos_dev_session", Value: sessionCookie})
	req.AddCookie(&http.Cookie{Name: workspaceHintCookie, Value: "invalid"})
	req.Header.Set("X-Workspace-ID", workspace.String())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func newSessionService(t *testing.T, db *storage.DB, cfg Config) *session.Service {
	t.Helper()
	service, err := session.New(db, session.Config{
		InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID,
		EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{"http://127.0.0.1:3000"},
		DevelopmentLoopback: true, CookieSecure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func insertSession(t *testing.T, db *storage.DB, cfg Config, person uuid.UUID) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO identity_sessions
			(id, person_id, installation_id, application_id, environment_id,
			 token_digest, security_epoch, authentication_method, authenticated_at,
			 expires_at, idle_expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,0,'email_password',$7,$8,$8)`,
			newID(t), person, cfg.InstallationID, cfg.ApplicationID,
			cfg.EnvironmentID, digest[:], now, now.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatalf("insert session fixture: %v", err)
	}
	return token
}

func newContextDB(t *testing.T) (*storage.DB, Config, uuid.UUID, uuid.UUID) {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	dbctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(dbctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "workspace.sql"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: string(workspaceSQL)}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(dbctx, db, registry); err != nil {
		t.Fatalf("apply isolated migrations: %v", err)
	}
	cfg := Config{InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t)}
	owner, member := newID(t), newID(t)
	if err := db.WithTx(dbctx, nil, func(tx *sql.Tx) error {
		for _, person := range []uuid.UUID{owner, member} {
			if _, err := tx.ExecContext(dbctx, `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, cfg.InstallationID, cfg.ApplicationID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create active people: %v", err)
	}
	return db, cfg, owner, member
}

func newPerson(t *testing.T, db *storage.DB, scope workspacestore.Scope) uuid.UUID {
	t.Helper()
	person := newID(t)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, scope.InstallationID, scope.ApplicationID)
		return err
	}); err != nil {
		t.Fatalf("create additional active person: %v", err)
	}
	return person
}

func createPersonal(t *testing.T, db *storage.DB, cfg Config, person uuid.UUID) workspacestore.Workspace {
	t.Helper()
	var workspace workspacestore.Workspace
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		workspace, err = store.CreatePersonalWorkspace(context.Background(), workspacestore.CreatePersonalInput{
			ID: newID(t), Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, OwnerPersonID: person,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create personal workspace: %v", err)
	}
	return workspace
}

func createOrganization(t *testing.T, db *storage.DB, scope workspacestore.Scope, owner uuid.UUID) workspacestore.Workspace {
	t.Helper()
	var workspace workspacestore.Workspace
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		workspace, _, err = store.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{
			ID: newID(t), OwnerMembershipID: newID(t),
			Scope: scope, OwnerPersonID: owner,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return workspace
}

func addMember(t *testing.T, db *storage.DB, cfg Config, workspaceID, person uuid.UUID) {
	t.Helper()
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		_, err = store.AddMembership(context.Background(), workspacestore.AddMembershipInput{
			ID: newID(t), Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID},
			WorkspaceID: workspaceID, PersonID: person, Role: workspacestore.RoleMember, RoleVersion: 1,
		})
		return err
	})
	if err != nil {
		t.Fatalf("add organization member: %v", err)
	}
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
