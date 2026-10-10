package context

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ajent-social/amos/identity"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestCurrentConstructorAndUnavailableBoundary(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	r, err := NewForTransactions(cfg)
	if err != nil || r.db != nil || r.cfg != cfg {
		t.Fatal("pool-free constructor did not preserve realm")
	}
	cfg.EnvironmentID = newID(t)
	if r.cfg == cfg {
		t.Fatal("constructor retained caller configuration")
	}
	for _, cfg := range []Config{{}, {InstallationID: newID(t)}, {newID(t), newID(t), uuid.New()}, {newID(t), uuid.Nil, newID(t)}} {
		if got, err := NewForTransactions(cfg); !errors.Is(err, ErrInvalidConfiguration) || got != nil {
			t.Fatal("invalid realm admitted")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		r    *Resolver
		ctx  context.Context
		tx   *sql.Tx
	}{
		{"nil resolver", nil, context.Background(), nil},
		{"nil context", r, nil, nil},
		{"nil transaction", r, context.Background(), nil},
		{"canceled", r, canceled, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := tc.r.ResolveCurrentTx(tc.ctx, tc.tx, identity.Principal{}, uuid.Nil)
			assertCurrentError(t, s, err, ErrUnavailable)
		})
	}
	called := false
	rec := httptest.NewRecorder()
	r.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://workspace.example.test/", nil))
	if called || rec.Code != http.StatusServiceUnavailable {
		t.Fatal("pool-free resolver silently admitted legacy middleware")
	}
}

func TestCurrentPersonValidation(t *testing.T) {
	for _, tc := range []struct {
		name, state     string
		epoch, supplied int64
		want            error
	}{
		{"active", "active", 5, 5, nil}, {"stale", "active", 6, 5, ErrDenied},
		{"negative stored", "active", -1, 0, ErrUnavailable}, {"negative supplied", "active", 0, -1, ErrUnavailable},
		{"pending", "pending_verification", 0, 0, ErrDenied}, {"disabled", "self_disabled", 0, 0, ErrDenied},
		{"admin disabled", "administratively_disabled", 0, 0, ErrDenied}, {"deletion", "deletion_pending", 0, 0, ErrDenied},
		{"unknown", "unknown", 0, 0, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCurrentPerson(tc.state, tc.epoch, tc.supplied); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestCurrentWorkspaceValidation(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person, id := newID(t), newID(t)
	base := workspacestore.Workspace{ID: id, Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, Kind: "personal", State: "active", PersonalOwnerID: person, Epoch: 2}
	for _, tc := range []struct {
		name    string
		mutate  func(*workspacestore.Workspace)
		present bool
		want    error
	}{
		{"personal", func(*workspacestore.Workspace) {}, true, nil},
		{"foreign owner", func(w *workspacestore.Workspace) { w.PersonalOwnerID = newID(t) }, true, ErrDenied},
		{"missing owner", func(*workspacestore.Workspace) {}, false, ErrUnavailable},
		{"invalid owner", func(w *workspacestore.Workspace) { w.PersonalOwnerID = uuid.New() }, true, ErrUnavailable},
		{"organization", func(w *workspacestore.Workspace) { w.Kind = "organization"; w.PersonalOwnerID = uuid.Nil }, false, nil},
		{"organization owner", func(w *workspacestore.Workspace) { w.Kind = "organization" }, true, ErrUnavailable},
		{"unknown kind", func(w *workspacestore.Workspace) { w.Kind = "other" }, true, ErrUnavailable},
		{"unknown state", func(w *workspacestore.Workspace) { w.State = "other" }, true, ErrUnavailable},
		{"suspended", func(w *workspacestore.Workspace) { w.State = "suspended" }, true, ErrDenied},
		{"deleting", func(w *workspacestore.Workspace) { w.State = "deletion_pending" }, true, ErrDenied},
		{"negative epoch", func(w *workspacestore.Workspace) { w.Epoch = -1 }, true, ErrUnavailable},
		{"wrong realm", func(w *workspacestore.Workspace) { w.Scope.ApplicationID = newID(t) }, true, ErrUnavailable},
		{"wrong ID", func(w *workspacestore.Workspace) { w.ID = newID(t) }, true, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := base
			tc.mutate(&w)
			if err := validateCurrentWorkspace(w, tc.present, cfg, id, person); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestCurrentMembershipValidation(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person, id := newID(t), newID(t)
	base := workspacestore.Membership{ID: newID(t), Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, WorkspaceID: id, PersonID: person, Role: "member", RoleVersion: 1, State: "active", Epoch: 7}
	for _, tc := range []struct {
		name   string
		mutate func(*workspacestore.Membership)
		want   error
	}{
		{"member", func(*workspacestore.Membership) {}, nil},
		{"owner", func(m *workspacestore.Membership) { m.Role = "owner" }, nil},
		{"admin new version", func(m *workspacestore.Membership) { m.Role = "admin"; m.RoleVersion = 2 }, nil},
		{"unknown role", func(m *workspacestore.Membership) { m.Role = "superuser" }, ErrUnavailable},
		{"zero version", func(m *workspacestore.Membership) { m.RoleVersion = 0 }, ErrUnavailable},
		{"negative epoch", func(m *workspacestore.Membership) { m.Epoch = -1 }, ErrUnavailable},
		{"missing ID", func(m *workspacestore.Membership) { m.ID = uuid.Nil }, ErrUnavailable},
		{"foreign person", func(m *workspacestore.Membership) { m.PersonID = newID(t) }, ErrUnavailable},
		{"foreign workspace", func(m *workspacestore.Membership) { m.WorkspaceID = newID(t) }, ErrUnavailable},
		{"foreign realm", func(m *workspacestore.Membership) { m.Scope.InstallationID = newID(t) }, ErrUnavailable},
		{"unknown state", func(m *workspacestore.Membership) { m.State = "unknown" }, ErrUnavailable},
		{"left", func(m *workspacestore.Membership) { m.State = "left" }, ErrDenied},
		{"suspended", func(m *workspacestore.Membership) { m.State = "suspended" }, ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			if err := validateCurrentMembership(m, cfg, id, person); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestCurrentPermissionDecodingAndCopy(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `[null]`, `[""]`, `["*"]`, `["workspace.members.read","workspace.members.read"]`, `[["billing.read"]]`, `[1]`, `{}`, `["billing.read"] trailing`} {
		t.Run(raw, func(t *testing.T) {
			if got, err := decodeCurrentPermissions([]byte(raw)); !errors.Is(err, ErrUnavailable) || got != nil {
				t.Fatal("malformed permission grant admitted")
			}
		})
	}
	raw := []byte(`["workspace.members.read","workspace.members.manage","workspace.owners.manage","workspace.authentication.manage","workspace.delete","billing.manage","billing.read"]`)
	permissions, err := decodeCurrentPermissions(raw)
	if err != nil || len(permissions) != 7 {
		t.Fatal("installed vocabulary rejected")
	}
	s := Selection{Membership: &workspacestore.Membership{Epoch: 3}, MembershipEpoch: 3, Permissions: permissions}
	copied := copySelection(s)
	copied.Membership.Epoch = 99
	copied.Permissions[0] = "changed"
	if s.Membership.Epoch != 3 || s.Permissions[0] != "workspace.members.read" {
		t.Fatal("selection aliases mutable output")
	}
	ctx := context.WithValue(context.Background(), selectionKey{}, s)
	from, ok := FromContext(ctx)
	if !ok {
		t.Fatal("legacy context unavailable")
	}
	from.Membership.Epoch = 88
	from.Permissions[0] = "changed again"
	again, _ := FromContext(ctx)
	if again.Membership.Epoch != 3 || again.Permissions[0] != "workspace.members.read" {
		t.Fatal("legacy context aliases selection")
	}
}

func assertCurrentError(t *testing.T, s Selection, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || !reflect.DeepEqual(s, Selection{}) {
		t.Fatalf("error=%v want=%v; zero selection=%v", err, want, reflect.DeepEqual(s, Selection{}))
	}
}
