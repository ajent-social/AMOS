package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/audit"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestAppendSharesMutationTransactionAndScopeCannotEnumerate(t *testing.T) {
	db, schema := testkit.NewPostgres(t)
	applyAuditMigration(t, schema)
	actor := audit.Actor{Kind: audit.ActorPerson, ID: newID(t)}
	store, err := New(db, testAuthorizer{allowAll: true}, testAuthorizer{allowAll: true}, testActorResolver{actor: actor})
	if err != nil {
		t.Fatal("construct audit store")
	}
	ctx := context.Background()
	event := validEvent(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE test_materials (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, state text NOT NULL)`); err != nil {
		t.Fatal("create synthetic material table")
	}

	rollbackTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin rollback transaction")
	}
	if _, err = rollbackTx.ExecContext(ctx, `INSERT INTO test_materials(id,workspace_id,state) VALUES ($1,$2,'created')`, event.ResourceID, event.WorkspaceID); err != nil {
		_ = rollbackTx.Rollback()
		t.Fatal("write synthetic material")
	}
	if _, err = store.AppendTx(ctx, rollbackTx, event); err != nil {
		_ = rollbackTx.Rollback()
		t.Fatalf("append event in mutation transaction: %v", err)
	}
	if err = rollbackTx.Rollback(); err != nil {
		t.Fatal("roll back combined transaction")
	}
	assertCount(t, db, "test_materials", 0)
	assertCount(t, db, "amos_security_audit_events", 0)

	commitTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin commit transaction")
	}
	if _, err = commitTx.ExecContext(ctx, `INSERT INTO test_materials(id,workspace_id,state) VALUES ($1,$2,'created')`, event.ResourceID, event.WorkspaceID); err != nil {
		_ = commitTx.Rollback()
		t.Fatal("write synthetic material")
	}
	created, err := store.AppendTx(ctx, commitTx, event)
	if err != nil {
		_ = commitTx.Rollback()
		t.Fatalf("append event in mutation transaction: %v", err)
	}
	if err := commitTx.Commit(); err != nil {
		t.Fatal("commit combined transaction")
	}
	assertCount(t, db, "test_materials", 1)
	assertCount(t, db, "amos_security_audit_events", 1)
	if created.CreatedAt.Location() != time.UTC {
		t.Fatal("record timestamp was not normalized to UTC")
	}

	page, err := store.List(ctx, scopeFor(event), nil, 10)
	if err != nil || len(page) != 1 || page[0].ID != created.ID || page[0].WorkspaceID != event.WorkspaceID || page[0].Actor != actor {
		t.Fatalf("same-scope list count=%d err=%v", len(page), err)
	}
	otherWorkspace := scopeFor(event)
	otherWorkspace.WorkspaceID = newID(t)
	denied, err := store.List(ctx, otherWorkspace, nil, 10)
	if err != nil || len(denied) != 0 {
		t.Fatalf("other-workspace result count=%d err=%v", len(denied), err)
	}
	otherApplication := scopeFor(event)
	otherApplication.ApplicationID = newID(t)
	denied, err = store.List(ctx, otherApplication, nil, 10)
	if err != nil || len(denied) != 0 {
		t.Fatalf("other-application result count=%d err=%v", len(denied), err)
	}
	badScope := scopeFor(event)
	badScope.WorkspaceID = nonRFCVariant(t)
	if _, err := store.List(ctx, badScope, nil, 10); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("non-RFC scope variant result=%v", err)
	}
	restricted, err := New(db, testAuthorizer{allowed: scopeFor(event)}, testAuthorizer{allowed: scopeFor(event)}, testActorResolver{actor: actor})
	if err != nil {
		t.Fatal("construct authorized audit reader")
	}
	if denied, err := restricted.List(ctx, otherWorkspace, nil, 10); !errors.Is(err, audit.ErrForbidden) || len(denied) != 0 {
		t.Fatalf("cross-workspace authorization result count=%d err=%v", len(denied), err)
	}
	blockedWriter, err := New(db, testAuthorizer{allowAll: true}, testAuthorizer{allowed: scopeFor(event)}, testActorResolver{actor: actor})
	if err != nil {
		t.Fatal("construct scoped audit writer")
	}
	blockedEvent := event
	blockedEvent.WorkspaceID = otherWorkspace.WorkspaceID
	deniedTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin denied append transaction")
	}
	if _, err := blockedWriter.AppendTx(ctx, deniedTx, blockedEvent); !errors.Is(err, audit.ErrForbidden) {
		_ = deniedTx.Rollback()
		t.Fatalf("cross-workspace append result=%v", err)
	}
	_ = deniedTx.Rollback()
	assertCount(t, db, "amos_security_audit_events", 1)
	if unavailable, err := New(db, testAuthorizer{failure: errors.New("synthetic membership store diagnostic")}, testAuthorizer{allowAll: true}, testActorResolver{actor: actor}); err != nil {
		t.Fatal("construct unavailable audit reader")
	} else if records, err := unavailable.List(ctx, scopeFor(event), nil, 10); !errors.Is(err, audit.ErrUnavailable) || len(records) != 0 {
		t.Fatalf("unavailable authorization result count=%d err=%v", len(records), err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE amos_security_audit_events SET outcome='denied' WHERE id=$1`, created.ID); err == nil {
		t.Fatal("append-only table accepted an update")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM amos_security_audit_events WHERE id=$1`, created.ID); err == nil {
		t.Fatal("append-only table accepted a delete")
	}
	if _, err := db.ExecContext(ctx, `TRUNCATE amos_security_audit_events`); err == nil {
		t.Fatal("append-only table accepted truncate")
	}
}

func TestListUsesBoundedStableCursor(t *testing.T) {
	db, schema := testkit.NewPostgres(t)
	applyAuditMigration(t, schema)
	store, err := New(db, testAuthorizer{allowAll: true}, testAuthorizer{allowAll: true}, testActorResolver{actor: audit.Actor{Kind: audit.ActorMachine, ID: newID(t)}})
	if err != nil {
		t.Fatal("construct audit store")
	}
	event := validEvent(t)
	for i := 0; i < 3; i++ {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal("begin append transaction")
		}
		event.ResourceID = newID(t)
		if _, err := store.AppendTx(context.Background(), tx, event); err != nil {
			_ = tx.Rollback()
			t.Fatalf("append synthetic event: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal("commit synthetic event")
		}
	}
	first, err := store.List(context.Background(), scopeFor(event), nil, 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page count=%d err=%v", len(first), err)
	}
	cursor := &audit.Cursor{CreatedAt: first[1].CreatedAt, ID: first[1].ID}
	second, err := store.List(context.Background(), scopeFor(event), cursor, 2)
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID || second[0].ID == first[1].ID {
		t.Fatalf("second page count=%d err=%v", len(second), err)
	}
	if _, err := store.List(context.Background(), scopeFor(event), nil, audit.MaxPageSize+1); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("oversized page result=%v", err)
	}
}

func TestAppendRejectsUnsafeMetadataWithoutDatabaseWrite(t *testing.T) {
	db, schema := testkit.NewPostgres(t)
	applyAuditMigration(t, schema)
	actor := audit.Actor{Kind: audit.ActorPerson, ID: newID(t)}
	store, err := New(db, testAuthorizer{allowAll: true}, testAuthorizer{allowAll: true}, testActorResolver{actor: actor})
	if err != nil {
		t.Fatal("construct audit store")
	}
	event := validEvent(t)
	event.Attributes = []audit.Attribute{{Key: "token", Value: "synthetic-secret-value"}}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal("begin transaction")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := store.AppendTx(context.Background(), tx, event); !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("unsafe attribute result=%v", err)
	}
	assertCount(t, db, "amos_security_audit_events", 0)
	for _, raw := range []string{
		`[{"key":"token","value":"synthetic-secret-value"}]`,
		`[{"key":"state","value":null}]`,
		`[{"key":"state","value":"created"},{"key":"state","value":"revoked"}]`,
	} {
		_, err = db.ExecContext(context.Background(), `INSERT INTO amos_security_audit_events
			(id,installation_id,application_id,environment_id,workspace_id,actor_kind,actor_id,action,resource_type,resource_id,outcome,correlation_id,attributes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)`,
			newID(t), event.InstallationID, event.ApplicationID, event.EnvironmentID, event.WorkspaceID,
			actor.Kind, actor.ID, event.Action, event.ResourceType, event.ResourceID,
			event.Outcome, event.CorrelationID, raw)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret-value") {
			t.Fatal("database audit guard accepted or echoed unsafe metadata")
		}
	}
	badActor := nonRFCVariant(t)
	_, err = db.ExecContext(context.Background(), `INSERT INTO amos_security_audit_events
		(id,installation_id,application_id,environment_id,workspace_id,actor_kind,actor_id,action,resource_type,resource_id,outcome,correlation_id,attributes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)`,
		newID(t), event.InstallationID, event.ApplicationID, event.EnvironmentID, event.WorkspaceID,
		actor.Kind, badActor, event.Action, event.ResourceType, event.ResourceID,
		event.Outcome, event.CorrelationID, `[]`)
	if err == nil {
		t.Fatal("database accepted a non-RFC UUID variant")
	}
	assertCount(t, db, "amos_security_audit_events", 0)
}

func applyAuditMigration(t *testing.T, schema string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "audit.sql"))
	if err != nil {
		t.Fatal("read audit migration fragment")
	}
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal("read configured PostgreSQL test URL")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse configured PostgreSQL test URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	migrationDB, err := storage.Open(context.Background(), parsed.String())
	if err != nil {
		t.Fatal("open isolated migration database")
	}
	t.Cleanup(func() {
		if err := migrationDB.Close(); err != nil {
			t.Error("close isolated migration database")
		}
	})
	// This isolated fragment fixture has its own test-only ledger. The real
	// reference composition keeps invocation migration at sequence 17.
	invocations, err := migrations.OperationInvocations(2)
	if err != nil {
		t.Fatal("load invocation audit extension")
	}
	registry, err := migrations.NewRegistry(migrations.Fragment{Namespace: "audit", Migrations: []migrations.Migration{{Sequence: 1, Name: "security_audit_events", SQL: string(contents)}}}, invocations)
	if err != nil {
		t.Fatal("construct isolated audit fixture registry")
	}
	if err := storage.Migrate(context.Background(), migrationDB, registry); err != nil {
		t.Fatal("apply owned audit migration fragment")
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal("count isolated test rows")
	}
	if got != want {
		t.Fatalf("isolated table row count=%d want=%d", got, want)
	}
}

func scopeFor(event audit.Event) audit.Scope {
	return audit.Scope{InstallationID: event.InstallationID, ApplicationID: event.ApplicationID, EnvironmentID: event.EnvironmentID, WorkspaceID: event.WorkspaceID}
}

func validEvent(t *testing.T) audit.Event {
	t.Helper()
	newID := func() uuid.UUID {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal("generate synthetic UUIDv7")
		}
		return id
	}
	return audit.Event{
		InstallationID: newID(), ApplicationID: newID(), EnvironmentID: newID(), WorkspaceID: newID(),
		Action:       audit.ActionMaterialCreated,
		ResourceType: audit.ResourceMaterial, ResourceID: newID(), Outcome: audit.OutcomeSucceeded,
		CorrelationID: newID(), Attributes: []audit.Attribute{{Key: "category", Value: "secret"}, {Key: "state", Value: "created"}},
	}
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate synthetic UUIDv7")
	}
	return id
}

func nonRFCVariant(t *testing.T) uuid.UUID {
	t.Helper()
	id := newID(t)
	id[8] = (id[8] & 0x3f) | 0x40
	if id.Version() != 7 || id.Variant() == uuid.RFC4122 {
		t.Fatal("invalid-variant UUID fixture is malformed")
	}
	return id
}

type testAuthorizer struct {
	allowed  audit.Scope
	allowAll bool
	failure  error
}

func (a testAuthorizer) AuthorizeAuditRead(_ context.Context, scope audit.Scope) error {
	return a.authorize(scope)
}

func (a testAuthorizer) AuthorizeAuditWrite(_ context.Context, scope audit.Scope) error {
	return a.authorize(scope)
}

func (a testAuthorizer) authorize(scope audit.Scope) error {
	if a.failure != nil {
		return a.failure
	}
	if a.allowAll || a.allowed == scope {
		return nil
	}
	return audit.ErrForbidden
}

type testActorResolver struct{ actor audit.Actor }

func (r testActorResolver) ResolveAuditActor(context.Context) (audit.Actor, error) {
	return r.actor, nil
}
