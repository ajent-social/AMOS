package store_test

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

	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestT4_2_TenantScopedWorkspacesMembershipAndPersonalUniqueness(t *testing.T) {
	db, scope, first, second := newWorkspaceDB(t)
	personalID := newID(t)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, e = s.CreatePersonalWorkspace(context.Background(), workspacestore.CreatePersonalInput{ID: personalID, Scope: scope, OwnerPersonID: first})
		return e
	}); err != nil {
		t.Fatalf("create personal workspace: %v", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, e = s.CreatePersonalWorkspace(context.Background(), workspacestore.CreatePersonalInput{ID: newID(t), Scope: scope, OwnerPersonID: first})
		return e
	}); !errors.Is(err, workspacestore.ErrPersonalWorkspaceExists) {
		t.Fatalf("duplicate personal workspace error=%v", err)
	}
	orgA := createOrg(t, db, scope, first)
	orgB := createOrg(t, db, scope, first)
	memberA := addMember(t, db, scope, orgA.ID, second, workspacestore.RoleAdmin)
	memberB := addMember(t, db, scope, orgB.ID, second, workspacestore.RoleMember)
	if memberA.Role != workspacestore.RoleAdmin || memberB.Role != workspacestore.RoleMember || memberA.WorkspaceID == memberB.WorkspaceID {
		t.Fatalf("membership roles/scopes were not kept separate: %#v %#v", memberA, memberB)
	}
	if _, err := getWorkspace(t, db, scope, personalID); err != nil {
		t.Fatalf("tenant-scoped lookup: %v", err)
	}
	wrongScope := workspacestore.Scope{InstallationID: newID(t), ApplicationID: scope.ApplicationID}
	if _, err := getWorkspace(t, db, wrongScope, orgA.ID); !errors.Is(err, workspacestore.ErrWorkspaceUnavailable) {
		t.Fatalf("cross-scope workspace lookup=%v", err)
	}
	otherScope := workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}
	otherPerson := newID(t)
	createPerson(t, db, otherScope, otherPerson)
	otherOrg := createOrg(t, db, otherScope, otherPerson)
	if _, err := getWorkspace(t, db, scope, otherOrg.ID); !errors.Is(err, workspacestore.ErrWorkspaceUnavailable) {
		t.Fatalf("workspace from another installation lookup=%v", err)
	}
	if _, err := getMembership(t, db, scope, otherOrg.ID, otherPerson); !errors.Is(err, workspacestore.ErrMembershipUnavailable) {
		t.Fatalf("membership from another installation lookup=%v", err)
	}
	if _, err := getMembership(t, db, scope, orgA.ID, second); err != nil {
		t.Fatalf("workspace A membership lookup: %v", err)
	}
	if _, err := getMembership(t, db, scope, orgB.ID, second); err != nil {
		t.Fatalf("workspace B membership lookup: %v", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, e = s.AddMembership(context.Background(), workspacestore.AddMembershipInput{ID: newID(t), Scope: scope, WorkspaceID: orgA.ID, PersonID: second, Role: workspacestore.RoleAdmin, RoleVersion: 1})
		return e
	}); !errors.Is(err, workspacestore.ErrMembershipExists) {
		t.Fatalf("duplicate membership error=%v", err)
	}
}

func TestT4_2_FailedOwnerMembershipRollsBackOrganization(t *testing.T) {
	db, scope, first, _ := newWorkspaceDB(t)
	org := createOrg(t, db, scope, first)
	// Use the existing owner's membership identifier to force the second insert
	// to fail after its organization row has already been inserted.
	owner, _ := getMembership(t, db, scope, org.ID, first)
	newOrgID := newID(t)
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, _, e = s.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{ID: newOrgID, OwnerMembershipID: owner.ID, Scope: scope, OwnerPersonID: first})
		return e
	})
	if !errors.Is(err, workspacestore.ErrMembershipExists) {
		t.Fatalf("duplicate owner membership error=%v", err)
	}
	if _, err = getWorkspace(t, db, scope, newOrgID); !errors.Is(err, workspacestore.ErrWorkspaceUnavailable) {
		t.Fatalf("partial organization remained after rollback: %v", err)
	}
}

func TestT4_2_ConcurrentOwnerDemotionsCannotLeaveActiveOrganizationOwnerless(t *testing.T) {
	db, scope, first, second := newWorkspaceDB(t)
	org := createOrg(t, db, scope, first)
	addMember(t, db, scope, org.ID, second, workspacestore.RoleOwner)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, person := range []uuid.UUID{first, second} {
		person := person
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				s, e := workspacestore.New(tx)
				if e != nil {
					return e
				}
				_, e = s.UpdateMembership(ctx, workspacestore.UpdateMembershipInput{Scope: scope, WorkspaceID: org.ID, PersonID: person, Role: workspacestore.RoleMember, RoleVersion: 1, State: workspacestore.MembershipActive})
				return e
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, failure := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			failure++
		}
	}
	if success != 1 || failure != 1 {
		t.Fatalf("concurrent owner demotions success=%d failed=%d, want exactly one each", success, failure)
	}
	var owners int
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM workspace_memberships WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND role_key='owner' AND state='active'`, scope.InstallationID, scope.ApplicationID, org.ID).Scan(&owners)
	}); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("active owner count=%d, want 1", owners)
	}
}

func TestT4_2_DisablingLastOwnerRequiresOrganizationSuspension(t *testing.T) {
	db, scope, owner, _ := newWorkspaceDB(t)
	org := createOrg(t, db, scope, owner)
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, owner)
		return err
	})
	if err == nil {
		t.Fatal("disabling the only active owner committed while the organization stayed active")
	}
	err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		if _, err = s.SetWorkspaceState(context.Background(), scope, org.ID, workspacestore.WorkspaceSuspended); err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, owner)
		return err
	})
	if err != nil {
		t.Fatalf("atomically suspend the organization and disable its only owner: %v", err)
	}
	got, err := getWorkspace(t, db, scope, org.ID)
	if err != nil || got.State != workspacestore.WorkspaceSuspended {
		t.Fatalf("workspace after emergency disable = %#v, %v", got, err)
	}
}

func TestT4_2_ConcurrentOwnerDisablementsCannotWriteSkew(t *testing.T) {
	db, scope, first, second := newWorkspaceDB(t)
	org := createOrg(t, db, scope, first)
	addMember(t, db, scope, org.ID, second, workspacestore.RoleOwner)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, person := range []uuid.UUID{first, second} {
		person := person
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			results <- db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, person)
				return err
			})
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, failure := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			failure++
		}
	}
	if success != 1 || failure != 1 {
		t.Fatalf("concurrent last-owner disablements success=%d failed=%d, want one each", success, failure)
	}
	var activeOwners int
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM workspace_memberships m JOIN identity_persons p ON p.id=m.person_id WHERE m.installation_id=$1 AND m.application_id=$2 AND m.workspace_id=$3 AND m.role_key='owner' AND m.state='active' AND p.state='active'`, scope.InstallationID, scope.ApplicationID, org.ID).Scan(&activeOwners)
	})
	if err != nil {
		t.Fatal(err)
	}
	if activeOwners != 1 {
		t.Fatalf("active person owner count=%d, want one", activeOwners)
	}
}

func TestT4_2_DisabledOrdinaryMemberRowIsRetainedButCannotAuthorize(t *testing.T) {
	db, scope, owner, member := newWorkspaceDB(t)
	org := createOrg(t, db, scope, owner)
	addMember(t, db, scope, org.ID, member, workspacestore.RoleMember)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, member)
		return err
	}); err != nil {
		t.Fatalf("disable ordinary member: %v", err)
	}
	if _, err := getMembership(t, db, scope, org.ID, member); !errors.Is(err, workspacestore.ErrMembershipUnavailable) {
		t.Fatalf("disabled person's membership read=%v, want unavailable", err)
	}
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		_, err = s.ReadMembershipEpoch(context.Background(), scope, org.ID, member)
		return err
	})
	if !errors.Is(err, workspacestore.ErrMembershipUnavailable) {
		t.Fatalf("disabled person's membership epoch read=%v", err)
	}
	var retained int
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM workspace_memberships WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4 AND state='active'`, scope.InstallationID, scope.ApplicationID, org.ID, member).Scan(&retained)
	}); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("active membership audit row count=%d, want retained row", retained)
	}
}

func TestT4_2_MembershipAdmissionLocksPersonAgainstDisable(t *testing.T) {
	db, scope, owner, member := newWorkspaceDB(t)
	org := createOrg(t, db, scope, owner)
	acquired := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	membershipID := newID(t)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result <- db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			s, err := workspacestore.New(tx)
			if err != nil {
				return err
			}
			if _, err = s.AddMembership(ctx, workspacestore.AddMembershipInput{ID: membershipID, Scope: scope, WorkspaceID: org.ID, PersonID: member, Role: workspacestore.RoleMember, RoleVersion: 1}); err != nil {
				return err
			}
			close(acquired)
			<-release
			return nil
		})
	}()
	select {
	case <-acquired:
	case err := <-result:
		t.Fatalf("membership did not reach locked transaction: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for membership transaction")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	lockErr := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var id uuid.UUID
		return tx.QueryRowContext(ctx, `SELECT id FROM identity_persons WHERE id=$1 FOR UPDATE NOWAIT`, member).Scan(&id)
	})
	cancel()
	var pgErr *pgconn.PgError
	if !errors.As(lockErr, &pgErr) || pgErr.Code != "55P03" {
		close(release)
		<-result
		t.Fatalf("person update lock did not conflict with admission's FOR SHARE: %v", lockErr)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("commit membership after admission serialized: %v", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, member)
		return err
	}); err != nil {
		t.Fatalf("disable ordinary member after admission: %v", err)
	}
	if _, err := getMembership(t, db, scope, org.ID, member); !errors.Is(err, workspacestore.ErrMembershipUnavailable) {
		t.Fatalf("membership lookup after person disable=%v, want unavailable", err)
	}
}

func newWorkspaceDB(t *testing.T) (*storage.DB, workspacestore.Scope, uuid.UUID, uuid.UUID) {
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
		t.Fatalf("open isolated Postgres: %v", err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
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
	registry, err := migrations.NewRegistry(migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}}, migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: string(workspaceSQL)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply isolated identity/workspace migrations: %v", err)
	}
	scope := workspacestore.Scope{InstallationID: newID(t), ApplicationID: newID(t)}
	first, second := newID(t), newID(t)
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		for _, person := range []uuid.UUID{first, second} {
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, scope.InstallationID, scope.ApplicationID); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create active-person fixtures: %v", err)
	}
	return db, scope, first, second
}
func createOrg(t *testing.T, db *storage.DB, scope workspacestore.Scope, owner uuid.UUID) workspacestore.Workspace {
	t.Helper()
	var org workspacestore.Workspace
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		org, _, e = s.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{ID: newID(t), OwnerMembershipID: newID(t), Scope: scope, OwnerPersonID: owner})
		return e
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return org
}
func createPerson(t *testing.T, db *storage.DB, scope workspacestore.Scope, person uuid.UUID) {
	t.Helper()
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, scope.InstallationID, scope.ApplicationID)
		return err
	}); err != nil {
		t.Fatalf("create tenant-scoped person fixture: %v", err)
	}
}
func addMember(t *testing.T, db *storage.DB, scope workspacestore.Scope, workspaceID, personID uuid.UUID, role string) workspacestore.Membership {
	t.Helper()
	var membership workspacestore.Membership
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		membership, e = s.AddMembership(context.Background(), workspacestore.AddMembershipInput{ID: newID(t), Scope: scope, WorkspaceID: workspaceID, PersonID: personID, Role: role, RoleVersion: 1})
		return e
	})
	if err != nil {
		t.Fatalf("add %s membership: %v", role, err)
	}
	return membership
}
func getWorkspace(t *testing.T, db *storage.DB, scope workspacestore.Scope, id uuid.UUID) (workspacestore.Workspace, error) {
	t.Helper()
	var w workspacestore.Workspace
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		w, e = s.FindWorkspace(context.Background(), scope, id)
		return e
	})
	return w, err
}
func getMembership(t *testing.T, db *storage.DB, scope workspacestore.Scope, workspaceID, personID uuid.UUID) (workspacestore.Membership, error) {
	t.Helper()
	var m workspacestore.Membership
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		m, e = s.FindMembership(context.Background(), scope, workspaceID, personID)
		return e
	})
	return m, err
}
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
