package context

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Required-service SOURCE: the independent operator supplies a precreated public
// identity/session/workspace schema and immutable role versions over runtime-only
// TLS. This suite does not create schema, alter roles, or open an admin connection.
func currentRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_WORKSPACE_CURRENT_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required workspace TLS PostgreSQL fixture absent: set AMOS_WORKSPACE_CURRENT_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("workspace runtime config unavailable")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("workspace runtime config close failed")
		}
	}()
	var config struct {
		Host           string `json:"host"`
		Port           uint16 `json:"port"`
		Database       string `json:"database"`
		User           string `json:"user"`
		Password       string `json:"password"`
		CAPath         string `json:"ca_path"`
		WrongHost      string `json:"wrong_host"`
		DMLTable       string `json:"dml_table"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		t.Fatal("workspace runtime config invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("workspace runtime config has trailing data")
	}
	roots, err := os.ReadFile(config.CAPath)
	if err != nil {
		t.Fatal("workspace runtime CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: config.Host, Port: config.Port, Database: config.Database, User: config.User, Password: config.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 3, MaxIdleConns: 3, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required workspace runtime TLS connection failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("workspace runtime close failed")
		}
	})
	return db
}

type currentRuntimeFixture struct {
	db                                                             *storage.RuntimeDB
	cfg                                                            Config
	person, owner, personal, foreignPersonal, organization, member uuid.UUID
	principal                                                      identity.Principal
	resolver                                                       *Resolver
}

func newCurrentRuntimeFixture(t *testing.T) currentRuntimeFixture {
	t.Helper()
	f := currentRuntimeFixture{db: currentRuntimeDB(t), cfg: Config{newID(t), newID(t), newID(t)}, person: newID(t), owner: newID(t), personal: newID(t), foreignPersonal: newID(t), organization: newID(t), member: newID(t)}
	var err error
	f.resolver, err = NewForTransactions(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Every cleanup predicate names this suite's newly allocated realm. No shared
	// role row or operator-owned schema is changed. Cleanup is separately bounded.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := f.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			for _, q := range []string{
				`UPDATE public.workspaces SET state='deletion_pending' WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.workspace_memberships WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.workspaces WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.identity_sessions WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.identity_persons WHERE installation_id=$1 AND application_id=$2`,
			} {
				if _, err := tx.ExecContext(ctx, q, f.cfg.InstallationID, f.cfg.ApplicationID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Error("exact-owned workspace runtime data cleanup failed")
		}
	})
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		for _, person := range []uuid.UUID{f.person, f.owner} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, f.cfg.InstallationID, f.cfg.ApplicationID); err != nil {
				return err
			}
		}
		st, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		scope := workspacestore.Scope{InstallationID: f.cfg.InstallationID, ApplicationID: f.cfg.ApplicationID}
		if _, err = st.CreatePersonalWorkspace(ctx, workspacestore.CreatePersonalInput{ID: f.personal, Scope: scope, OwnerPersonID: f.person}); err != nil {
			return err
		}
		if _, err = st.CreatePersonalWorkspace(ctx, workspacestore.CreatePersonalInput{ID: f.foreignPersonal, Scope: scope, OwnerPersonID: f.owner}); err != nil {
			return err
		}
		if _, _, err = st.CreateOrganizationWorkspace(ctx, workspacestore.CreateOrganizationInput{ID: f.organization, OwnerMembershipID: newID(t), Scope: scope, OwnerPersonID: f.owner}); err != nil {
			return err
		}
		if _, err = st.AddMembership(ctx, workspacestore.AddMembershipInput{ID: f.member, Scope: scope, WorkspaceID: f.organization, PersonID: f.person, Role: "member", RoleVersion: 1}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,authenticated_at,expires_at,idle_expires_at)
   VALUES($1,$2,$3,$4,$5,$6,0,'email_password',pg_catalog.clock_timestamp()-interval '1 minute',pg_catalog.clock_timestamp()+interval '1 hour',pg_catalog.clock_timestamp()+interval '20 minutes')`, newID(t), f.person, f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, digest[:])
		return err
	})
	// A synthetic session is read through the public constructor/middleware. It is
	// only test input for workspace facts, not proof of any native login producer.
	f.principal = currentPrincipalFromSession(t, f.db, f.cfg, token)
	return f
}
func currentRuntimeTx(t *testing.T, db *storage.RuntimeDB, opts *sql.TxOptions, fn func(context.Context, *sql.Tx) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if opts == nil {
		opts = &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	}
	if err := db.WithTx(ctx, opts, func(tx *sql.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal("required workspace runtime transaction failed")
	}
}

func TestWorkspaceCurrentRuntimeSelections(t *testing.T) {
	f := newCurrentRuntimeFixture(t)
	for _, selector := range []uuid.UUID{uuid.Nil, f.personal, f.organization} {
		currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
			s, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, selector)
			if err != nil {
				return err
			}
			want := selector
			if want == uuid.Nil {
				want = f.personal
			}
			if s.Workspace.ID != want {
				t.Error("wrong current workspace")
			}
			if want == f.organization {
				if s.Membership == nil || s.Membership.ID != f.member || !reflect.DeepEqual(s.Permissions, []string{"workspace.members.read"}) {
					t.Error("current membership or permissions missing")
				}
				s.Permissions[0] = "changed"
				s.Membership.Epoch = 100
			}
			again, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, want)
			if err != nil {
				return err
			}
			if again.Workspace.ID != want || again.MembershipEpoch != 0 {
				t.Error("pinned selection changed")
			}
			if want == f.organization && (!reflect.DeepEqual(again.Permissions, []string{"workspace.members.read"}) || again.Membership.Epoch != 0) {
				t.Error("returned data changed database authority")
			}
			return nil
		})
	}
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		s, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, newID(t))
		assertCurrentError(t, s, err, ErrDenied)
		s, err = f.resolver.ResolveCurrentTx(ctx, tx, f.principal, f.foreignPersonal)
		assertCurrentError(t, s, err, ErrDenied)
		cfg := f.cfg
		cfg.EnvironmentID = newID(t)
		other, err := NewForTransactions(cfg)
		if err != nil {
			return err
		}
		s, err = other.ResolveCurrentTx(ctx, tx, f.principal, f.personal)
		assertCurrentError(t, s, err, ErrDenied)
		cfg = f.cfg
		cfg.InstallationID = newID(t)
		other, err = NewForTransactions(cfg)
		if err != nil {
			return err
		}
		s, err = other.ResolveCurrentTx(ctx, tx, f.principal, f.personal)
		assertCurrentError(t, s, err, ErrDenied)
		return nil
	})
}

func TestWorkspaceCurrentRuntimeDenials(t *testing.T) {
	f := newCurrentRuntimeFixture(t)
	// Changes apply only to owned rows and are rolled back after each assertion.
	rollback := errors.New("rollback case")
	for _, tc := range []struct {
		name, sql string
		arg       uuid.UUID
		selector  uuid.UUID
	}{
		{"person epoch", `UPDATE public.identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, f.person, f.personal},
		{"person disabled", `UPDATE public.identity_persons SET state='self_disabled' WHERE id=$1`, f.person, f.organization},
		{"person pending", `UPDATE public.identity_persons SET state='pending_verification' WHERE id=$1`, f.person, f.personal},
		{"workspace suspended", `UPDATE public.workspaces SET state='suspended' WHERE id=$1`, f.personal, f.personal},
		{"default absent", `DELETE FROM public.workspaces WHERE id=$1`, f.personal, uuid.Nil},
		{"organization suspended", `UPDATE public.workspaces SET state='suspended' WHERE id=$1`, f.organization, f.organization},
		{"default suspended", `UPDATE public.workspaces SET state='suspended' WHERE id=$1`, f.personal, uuid.Nil},
		{"member suspended", `UPDATE public.workspace_memberships SET state='suspended',membership_epoch=membership_epoch+1 WHERE id=$1`, f.member, f.organization},
		{"member left", `UPDATE public.workspace_memberships SET state='left',membership_epoch=membership_epoch+1 WHERE id=$1`, f.member, f.organization},
		{"member removed", `DELETE FROM public.workspace_memberships WHERE id=$1`, f.member, f.organization},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := f.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, tc.sql, tc.arg); err != nil {
					return err
				}
				s, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, tc.selector)
				assertCurrentError(t, s, err, ErrDenied)
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("required owned-row denial setup failed")
			}
		})
	}
	for _, opts := range []*sql.TxOptions{{Isolation: sql.LevelRepeatableRead}, {Isolation: sql.LevelSerializable}, {Isolation: sql.LevelReadCommitted, ReadOnly: true}} {
		currentRuntimeTx(t, f.db, opts, func(ctx context.Context, tx *sql.Tx) error {
			s, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, f.personal)
			assertCurrentError(t, s, err, ErrUnavailable)
			return nil
		})
	}
	var closed *sql.Tx
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		closed = tx
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		s, err := f.resolver.ResolveCurrentTx(canceled, tx, f.principal, f.personal)
		assertCurrentError(t, s, err, ErrUnavailable)
		return nil
	})
	s, err := f.resolver.ResolveCurrentTx(context.Background(), closed, f.principal, f.personal)
	assertCurrentError(t, s, err, ErrUnavailable)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT 1/0`); err == nil {
			t.Fatal("expected aborted-transaction setup")
		}
		s, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, f.personal)
		assertCurrentError(t, s, err, ErrUnavailable)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("aborted transaction case failed")
	}
}

func TestWorkspaceCurrentRuntimeLocksRetained(t *testing.T) {
	f := newCurrentRuntimeFixture(t)
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := f.resolver.ResolveCurrentTx(ctx, tx, f.principal, f.organization); err != nil {
			return err
		}
		for _, q := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`SELECT id FROM public.identity_persons WHERE id=$1 FOR UPDATE NOWAIT`, f.person},
			{`SELECT id FROM public.workspaces WHERE id=$1 FOR UPDATE NOWAIT`, f.organization},
			{`SELECT id FROM public.workspace_memberships WHERE id=$1 FOR UPDATE NOWAIT`, f.member},
		} {
			err := f.db.WithTx(ctx, nil, func(writer *sql.Tx) error {
				var id uuid.UUID
				return writer.QueryRowContext(ctx, q.sql, q.id).Scan(&id)
			})
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatal("required SHARE row lock was not retained")
			}
		}
		return nil
	})
	// Once the caller commits, those same rows can be acquired in P/W/M order.
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`SELECT id FROM public.identity_persons WHERE id=$1 FOR UPDATE NOWAIT`, f.person},
			{`SELECT id FROM public.workspaces WHERE id=$1 FOR UPDATE NOWAIT`, f.organization},
			{`SELECT id FROM public.workspace_memberships WHERE id=$1 FOR UPDATE NOWAIT`, f.member},
		} {
			var id uuid.UUID
			if err := tx.QueryRowContext(ctx, q.sql, q.id).Scan(&id); err != nil {
				return err
			}
		}
		return nil
	})
}

func TestWorkspaceCurrentRuntimeWriterBeforeReader(t *testing.T) {
	f := newCurrentRuntimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	readerPID := make(chan int, 1)
	readerDone := make(chan error, 1)
	readerStarted := false
	defer func() {
		cancel()
		if readerStarted {
			select {
			case <-readerDone:
			case <-time.After(3 * time.Second):
				t.Error("canceled reader failed to drain")
			}
		}
	}()
	writerErr := f.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(writer *sql.Tx) error {
		var id uuid.UUID
		if err := writer.QueryRowContext(ctx, `SELECT id FROM public.identity_persons WHERE id=$1 FOR UPDATE`, f.person).Scan(&id); err != nil {
			return err
		}
		if err := writer.QueryRowContext(ctx, `SELECT id FROM public.workspaces WHERE id=$1 FOR UPDATE`, f.organization).Scan(&id); err != nil {
			return err
		}
		if _, err := writer.ExecContext(ctx, `UPDATE public.workspace_memberships SET state='suspended',membership_epoch=membership_epoch+1 WHERE id=$1`, f.member); err != nil {
			return err
		}
		readerStarted = true
		go func() {
			readerDone <- f.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(reader *sql.Tx) error {
				var pid int
				if err := reader.QueryRowContext(ctx, `SELECT pg_catalog.pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				readerPID <- pid
				s, err := f.resolver.ResolveCurrentTx(ctx, reader, f.principal, f.organization)
				if !errors.Is(err, ErrDenied) || !reflect.DeepEqual(s, Selection{}) {
					return errors.New("post-wait membership was not denied with zero output")
				}
				return nil
			})
		}()
		var pid int
		select {
		case pid = <-readerPID:
		case <-ctx.Done():
			return ctx.Err()
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if err := writer.QueryRowContext(ctx, `SELECT pg_catalog.cardinality(pg_catalog.pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				return nil
			} // commit writer only after a real lock wait is observed
			select {
			case err := <-readerDone:
				readerStarted = false
				if err == nil {
					return errors.New("reader bypassed held person UPDATE")
				}
				return err
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
	if writerErr != nil {
		t.Fatal("writer-before-reader schedule failed")
	}
	select {
	case err := <-readerDone:
		readerStarted = false
		if err != nil {
			t.Fatal("reader did not deny committed post-wait membership")
		}
	case <-ctx.Done():
		t.Fatal("reader did not complete after writer commit")
	}
}
