package personal

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestT4_3_RejectsRequestedOwnerDifferentFromVerifiedPrincipal(t *testing.T) {
	participant := &Participant{tx: &sql.Tx{}}
	_, err := participant.Bootstrap(context.Background(), identity.Principal{}, Input{
		WorkspaceID: newID(t), Scope: workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}, OwnerID: newID(t),
	})
	if !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("mismatched or absent verified owner error = %v, want %v", err, ErrOwnerMismatch)
	}
}

func TestT4_3_ConcurrentAndRepeatedBootstrapIsIdempotent(t *testing.T) {
	db, scope, person := newPersonalDB(t)
	const attempts = 8
	var wg sync.WaitGroup
	results := make(chan Result, attempts)
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var result Result
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				participant := &Participant{tx: tx}
				var e error
				result, e = participant.bootstrapActivePerson(ctx, Input{WorkspaceID: newID(t), Scope: scope, OwnerID: person}, person, 0)
				return e
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent bootstrap: %v", err)
	}
	var workspaceID uuid.UUID
	count := 0
	for result := range results {
		count++
		if result.Workspace.PersonalOwnerID != person || result.Workspace.Kind != workspacestore.KindPersonal {
			t.Errorf("bootstrap returned workspace with wrong owner or kind: %#v", result.Workspace)
		}
		if workspaceID == uuid.Nil {
			workspaceID = result.Workspace.ID
		} else if result.Workspace.ID != workspaceID {
			t.Errorf("concurrent retries returned different workspace IDs: %s and %s", workspaceID, result.Workspace.ID)
		}
	}
	if count != attempts {
		t.Fatalf("successful bootstrap results=%d, want %d", count, attempts)
	}
	var stored int
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM workspaces WHERE installation_id=$1 AND application_id=$2 AND personal_owner_id=$3 AND kind='personal'`, scope.InstallationID, scope.ApplicationID, person).Scan(&stored)
	}); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("personal workspace count=%d, want 1", stored)
	}
}

func TestT4_3_BootstrapRollsBackWithSignupTransaction(t *testing.T) {
	db, scope, person := newPersonalDB(t)
	rollback := errors.New("force signup rollback")
	requestedID := newID(t)
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		p := &Participant{tx: tx}
		if _, err := p.bootstrapActivePerson(context.Background(), Input{WorkspaceID: requestedID, Scope: scope, OwnerID: person}, person, 0); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("transaction error=%v, want forced rollback", err)
	}
	var count int
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM workspaces WHERE id=$1`, requestedID).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back workspace count=%d, want 0", count)
	}
}

func TestT4_3_DetectsSuspendedHistoricalWorkspaceForRepair(t *testing.T) {
	db, scope, person := newPersonalDB(t)
	created := bootstrapPerson(t, db, scope, person)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE workspaces SET state='suspended' WHERE id=$1`, created.Workspace.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		p := &Participant{tx: tx}
		_, err := p.bootstrapActivePerson(context.Background(), Input{WorkspaceID: newID(t), Scope: scope, OwnerID: person}, person, 0)
		return err
	})
	if !errors.Is(err, ErrWorkspaceRepairRequired) {
		t.Fatalf("suspended historical workspace error=%v, want repair required", err)
	}
}

func bootstrapPerson(t *testing.T, db *storage.DB, scope workspacestore.Scope, person uuid.UUID) Result {
	t.Helper()
	var result Result
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		var err error
		result, err = (&Participant{tx: tx}).bootstrapActivePerson(context.Background(), Input{WorkspaceID: newID(t), Scope: scope, OwnerID: person}, person, 0)
		return err
	})
	if err != nil {
		t.Fatalf("bootstrap person: %v", err)
	}
	return result
}

func newPersonalDB(t *testing.T) (*storage.DB, workspacestore.Scope, uuid.UUID) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
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
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply isolated migrations: %v", err)
	}
	scope := workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}
	person := newID(t)
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, scope.InstallationID, scope.ApplicationID)
		return err
	}); err != nil {
		t.Fatalf("create active person fixture: %v", err)
	}
	return db, scope, person
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
