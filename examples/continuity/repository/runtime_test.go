package repository_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/repository"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Operator-precreated schema and runtime-only TLS credentials are mandatory.
// Every repository error is returned to WithTx, which rolls the caller back.
func continuityRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_CONTINUITY_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_CONTINUITY_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required runtime fixture config cannot be opened")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime fixture config close failed")
		}
	}()
	var cfg struct {
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
	if err := dec.Decode(&cfg); err != nil {
		t.Fatal("required runtime fixture JSON invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("required runtime fixture JSON has trailing data")
	}
	roots, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		t.Fatal("required runtime fixture CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required runtime TLS PostgreSQL connection failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime database close failed")
		}
	})
	return db
}

type fixture struct {
	scope                                                                      repository.Scope
	property, source, caseID, application, procedure, support, decision, actor string
}

func freshID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate test ID failed")
	}
	return id.String()
}
func freshScope(t *testing.T) repository.Scope {
	t.Helper()
	return repository.Scope{InstallationID: uuid.MustParse(freshID(t)), ApplicationID: uuid.MustParse(freshID(t)), EnvironmentID: uuid.MustParse(freshID(t)), WorkspaceID: uuid.MustParse(freshID(t))}
}
func args(s repository.Scope, v ...any) []any {
	return append([]any{s.InstallationID, s.ApplicationID, s.EnvironmentID, s.WorkspaceID}, v...)
}

const scopeSQL = `installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4`

func transact(db *storage.RuntimeDB, s repository.Scope, fn func(context.Context, *repository.Repository) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		r, err := repository.New(tx, s)
		if err != nil {
			return err
		}
		return fn(ctx, r)
	})
}
func seed(t *testing.T, db *storage.RuntimeDB) fixture {
	t.Helper()
	f := fixture{scope: freshScope(t), property: freshID(t), source: freshID(t), caseID: freshID(t), application: freshID(t), procedure: freshID(t), support: freshID(t), decision: freshID(t), actor: freshID(t)}
	body := "Untrusted <b>source</b> 100%_literal"
	sum := sha256.Sum256([]byte(body))
	sources, err := json.Marshal([]string{f.source})
	if err != nil {
		t.Fatal(err)
	}
	items, err := json.Marshal([]domain.ChecklistItem{{ID: f.support, Label: "Supporting record"}, {ID: f.decision, Label: "Human decision", HumanDecision: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := boundedContext(t)
	err = db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		queries := []struct {
			q string
			v []any
		}{
			{`INSERT INTO public.continuity_properties (installation_id,application_id,environment_id,workspace_id,id,name,area,owner_label,occupant_label,occupancy,inspection_date) VALUES ($1,$2,$3,$4,$5,'Register 100%_literal','North','Owner','Occupant','occupied','2024-02-29')`, args(f.scope, f.property)},
			{`INSERT INTO public.continuity_sources (installation_id,application_id,environment_id,workspace_id,id,kind,title,body,sha256) VALUES ($1,$2,$3,$4,$5,'document','Source',$6,$7)`, args(f.scope, f.source, body, hex.EncodeToString(sum[:]))},
			{`INSERT INTO public.continuity_cases (installation_id,application_id,environment_id,workspace_id,id,property_id,title,status,revision,source_ids) VALUES ($1,$2,$3,$4,$5,$6,'Case','awaiting_owner',1,$7)`, args(f.scope, f.caseID, f.property, sources)},
			{`INSERT INTO public.continuity_applications (installation_id,application_id,environment_id,workspace_id,id,property_id,revision,items) VALUES ($1,$2,$3,$4,$5,$6,1,$7)`, args(f.scope, f.application, f.property, items)},
			{`INSERT INTO public.continuity_procedures (installation_id,application_id,environment_id,workspace_id,id,title,body,revision) VALUES ($1,$2,$3,$4,$5,'Procedure','Existing plain text',1)`, args(f.scope, f.procedure)},
		}
		for _, q := range queries {
			if _, e := tx.ExecContext(ctx, q.q, q.v...); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal("required runtime schema/seed unavailable")
	}
	// Rows are owned by this random scope; cleanup is runtime DML, never DDL.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			for _, table := range []string{"continuity_activity", "continuity_drafts", "continuity_cases", "continuity_applications", "continuity_procedures", "continuity_sources", "continuity_properties"} {
				if _, err := tx.ExecContext(ctx, `DELETE FROM public.`+table+` WHERE `+scopeSQL, args(f.scope)...); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Error("owned runtime rows cleanup failed")
		}
	})
	return f
}
func requireError[T any](t *testing.T, got T, err, want error) {
	t.Helper()
	var zero T
	if !errors.Is(err, want) || !reflect.DeepEqual(got, zero) {
		t.Fatalf("expected zero output and %v, got %v", want, err)
	}
}
func TestRepositoryRequiredService(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	ctx := boundedContext(t)
	t.Run("bounded discovery and literal search", func(t *testing.T) {
		err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			p, e := r.Properties(ctx, "", "%_", 1)
			if e != nil {
				return e
			}
			if len(p) != 1 || p[0].ID != f.property || p[0].InspectionDate != "2024-02-29" {
				t.Fatal("property search/date failed")
			}
			p, e = r.Properties(ctx, f.property, "", 1)
			if e != nil {
				return e
			}
			if p == nil || len(p) != 0 {
				t.Fatal("keyset empty page differs")
			}
			p, e = r.Properties(ctx, "", "not present", 100)
			if e != nil {
				return e
			}
			if len(p) != 0 {
				t.Fatal("search not literal")
			}
			s, e := r.Sources(ctx, "", "%_", "document", 100)
			if e != nil {
				return e
			}
			if len(s) != 1 || s[0].ID != f.source {
				t.Fatal("source summary missing")
			}
			src, e := r.Source(ctx, f.source)
			if e != nil {
				return e
			}
			if src.SHA256 != s[0].SHA256 {
				t.Fatal("source digest mismatch")
			}
			c, e := r.Cases(ctx, "", 100)
			if e != nil {
				return e
			}
			if len(c) != 1 {
				t.Fatal("case listing")
			}
			a, e := r.Applications(ctx, "", 100)
			if e != nil {
				return e
			}
			if len(a) != 1 {
				t.Fatal("application listing")
			}
			p2, e := r.Procedures(ctx, "", 100)
			if e != nil {
				return e
			}
			if len(p2) != 1 {
				t.Fatal("procedure listing")
			}
			activity, e := r.Activity(ctx, "", 100)
			if e != nil {
				return e
			}
			if activity == nil || len(activity) != 0 {
				t.Fatal("initial activity")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("each scope dimension denies disclosure and writes", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			foreign := f.scope
			switch i {
			case 0:
				foreign.InstallationID = uuid.MustParse(freshID(t))
			case 1:
				foreign.ApplicationID = uuid.MustParse(freshID(t))
			case 2:
				foreign.EnvironmentID = uuid.MustParse(freshID(t))
			case 3:
				foreign.WorkspaceID = uuid.MustParse(freshID(t))
			}
			err := transact(db, foreign, func(ctx context.Context, r *repository.Repository) error {
				v, e := r.Case(ctx, f.caseID)
				requireError(t, v, e, repository.ErrNotFound)
				return e
			})
			if err != repository.ErrNotFound {
				t.Fatal(err)
			}
			err = transact(db, foreign, func(ctx context.Context, r *repository.Repository) error {
				v, e := r.ChangeCase(ctx, f.caseID, 1, domain.Completed, f.actor)
				requireError(t, v, e, repository.ErrNotFound)
				return e
			})
			if err != repository.ErrNotFound {
				t.Fatal(err)
			}
			if err = transact(db, foreign, func(ctx context.Context, r *repository.Repository) error {
				v, e := r.Properties(ctx, "", "", 100)
				if e == nil && (v == nil || len(v) != 0) {
					t.Fatal("foreign list disclosed")
				}
				return e
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("mutations commit with activity", func(t *testing.T) {
		if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.ChangeCase(ctx, f.caseID, 1, domain.InProgress, f.actor)
			if e == nil && (v.Revision != 2 || v.Status != domain.InProgress) {
				t.Fatal("case mutation")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.SetChecklist(ctx, f.application, 1, f.support, true, f.actor)
			if e == nil && (!v.Items[0].Done || v.Revision != 2) {
				t.Fatal("checklist mutation")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.EditProcedure(ctx, f.procedure, 1, " Revised\nprocedure ", f.actor)
			if e == nil && (v.Revision != 2 || v.Body != "Revised\nprocedure") {
				t.Fatal("procedure mutation")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{"First unsent draft", "Second unsent draft"} {
			if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
				v, e := r.SaveDraft(ctx, f.caseID, 2, body, f.actor)
				if e == nil && (v.CaseRevision != 2 || v.Body != body) {
					t.Fatal("draft mutation")
				}
				return e
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			d, e := r.Draft(ctx, f.caseID)
			if e != nil {
				return e
			}
			if d.Body != "Second unsent draft" || len(d.SourceIDs) != 1 {
				t.Fatal("draft not persisted")
			}
			a, e := r.Activity(ctx, "", 100)
			if e != nil {
				return e
			}
			if len(a) != 5 {
				t.Fatal("activity not atomic or duplicate draft retried")
			}
			for _, v := range a {
				if v.ActorID != f.actor {
					t.Fatal("actor selector")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("stale and disabled zero outputs", func(t *testing.T) {
		err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.ChangeCase(ctx, f.caseID, 1, domain.Completed, f.actor)
			requireError(t, v, e, repository.ErrConflict)
			return e
		})
		if err != repository.ErrConflict {
			t.Fatal(err)
		}
		err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.SetChecklist(ctx, f.application, 2, f.decision, true, f.actor)
			requireError(t, v, e, repository.ErrDecisionDisabled)
			return e
		})
		if err != repository.ErrDecisionDisabled {
			t.Fatal(err)
		}
		err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.SaveDraft(ctx, f.caseID, 1, "stale", f.actor)
			requireError(t, v, e, repository.ErrConflict)
			return e
		})
		if err != repository.ErrConflict {
			t.Fatal(err)
		}
	})
	t.Run("caller rollback restores both writes", func(t *testing.T) {
		marker := errors.New("caller rejected commit")
		err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			if _, e := r.ChangeCase(ctx, f.caseID, 2, domain.Completed, f.actor); e != nil {
				return e
			}
			return marker
		})
		if err != marker {
			t.Fatal(err)
		}
		if err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			v, e := r.Case(ctx, f.caseID)
			if e != nil {
				return e
			}
			a, e := r.Activity(ctx, "", 100)
			if e == nil && (v.Revision != 2 || len(a) != 5) {
				t.Fatal("caller rollback leaked mutation/activity")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("two caller CAS contention", func(t *testing.T) {
		start := make(chan struct{})
		out := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				out <- transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
					_, e := r.ChangeCase(ctx, f.caseID, 2, domain.Completed, f.actor)
					return e
				})
			}()
		}
		close(start)
		wg.Wait()
		close(out)
		successes, conflicts := 0, 0
		for e := range out {
			switch e {
			case nil:
				successes++
			case repository.ErrConflict:
				conflicts++
			default:
				t.Fatal(e)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatal("CAS did not select exactly one winner")
		}
	})
	t.Run("same scope source required and corruption withheld", func(t *testing.T) {
		// Deliberately altered rows are rolled back by the test caller, never fixture DDL.
		if err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.source, hex.EncodeToString(make([]byte, 32)))...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			v, e := r.Source(ctx, f.source)
			requireError(t, v, e, repository.ErrUnavailable)
			return e
		}); err != repository.ErrUnavailable {
			t.Fatal("corruption test failed")
		}
		if err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			raw, e := json.Marshal([]string{freshID(t)})
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE public.continuity_cases SET source_ids=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.caseID, raw)...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			v, e := r.Case(ctx, f.caseID)
			requireError(t, v, e, repository.ErrNotFound)
			return e
		}); err != repository.ErrNotFound {
			t.Fatal("missing source test failed")
		}
	})
}

func TestRepositoryRequiredServiceActivityFailure(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `LOCK TABLE public.continuity_activity IN ACCESS EXCLUSIVE MODE`); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-finished:
		t.Fatalf("runtime activity lock unavailable: %v", err)
	case <-ctx.Done():
		t.Fatal("activity lock deadline")
	}
	err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='150ms'`); err != nil {
			return err
		}
		r, err := repository.New(tx, f.scope)
		if err != nil {
			return err
		}
		got, err := r.EditProcedure(ctx, f.procedure, 1, "must roll back", f.actor)
		// The procedure UPDATE is unblocked; the activity INSERT alone hits the lock.
		var zero domain.Procedure
		if err != repository.ErrUnavailable || got != zero {
			return errors.New("activity failure did not produce zero/unavailable")
		}
		return err // the caller must roll back, even after the successful UPDATE
	})
	close(release)
	lockErr := <-finished
	if lockErr != nil {
		t.Fatal("activity lock transaction failed")
	}
	if err != repository.ErrUnavailable {
		t.Fatal(err)
	}
	if err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
		p, e := r.Procedure(ctx, f.procedure)
		if e != nil {
			return e
		}
		a, e := r.Activity(ctx, "", 100)
		if e != nil {
			return e
		}
		if p.Revision != 1 || p.Body != "Existing plain text" || len(a) != 0 {
			t.Fatal("failed activity persisted partial mutation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRequiredServiceJSONBound(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	ctx := boundedContext(t)
	// A valid array cardinality must not admit an arbitrarily large nested value.
	raw, e := json.Marshal([]map[string]string{{"ID": f.support, "Label": strings.Repeat("x", 70000)}})
	if e != nil {
		t.Fatal(e)
	}
	err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE public.continuity_applications SET items=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.application, raw)...)
		return e
	})
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23514" {
		t.Fatal("oversized JSON did not fail its database check constraint")
	}
}

func TestRepositoryRequiredServiceValidation(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	foreign := seed(t, db)
	ctx := boundedContext(t)
	t.Run("foreign source cannot resolve", func(t *testing.T) {
		raw, err := json.Marshal([]string{foreign.source})
		if err != nil {
			t.Fatal(err)
		}
		err = db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `UPDATE public.continuity_cases SET source_ids=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.caseID, raw)...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			v, e := r.Cases(ctx, "", 100)
			requireError(t, v, e, repository.ErrNotFound)
			return e
		})
		if err != repository.ErrNotFound {
			t.Fatal("foreign source disclosure was not denied")
		}
	})
	t.Run("same scope property foreign key", func(t *testing.T) {
		err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `UPDATE public.continuity_cases SET property_id=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.caseID, foreign.property)...)
			return e
		})
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23503" {
			t.Fatal("foreign property was not rejected by scoped foreign key")
		}
	})
	t.Run("source summary validates body hash", func(t *testing.T) {
		err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `UPDATE public.continuity_sources SET body='changed without digest' WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.source)...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			v, e := r.Sources(ctx, "", "", "", 100)
			requireError(t, v, e, repository.ErrUnavailable)
			return e
		})
		if err != repository.ErrUnavailable {
			t.Fatal("corrupt summary acquired trusted digest")
		}
	})
	t.Run("unknown checklist field", func(t *testing.T) {
		err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `UPDATE public.continuity_applications SET items=jsonb_set(items,'{0,unknown}','true') WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.application)...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			v, e := r.Application(ctx, f.application)
			requireError(t, v, e, repository.ErrUnavailable)
			return e
		})
		if err != repository.ErrUnavailable {
			t.Fatal("unknown persisted checklist field admitted")
		}
	})
	t.Run("bounded keyset pages", func(t *testing.T) {
		marker := errors.New("test rollback")
		err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
			id := freshID(t)
			if _, e := tx.ExecContext(ctx, `INSERT INTO public.continuity_properties (installation_id,application_id,environment_id,workspace_id,id,name,area,owner_label,occupant_label,occupancy) VALUES ($1,$2,$3,$4,$5,'Second','South','Owner','Occupant','unknown')`, args(f.scope, id)...); e != nil {
				return e
			}
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			one, e := r.Properties(ctx, "", "", 1)
			if e != nil {
				return e
			}
			if len(one) != 1 {
				t.Fatal("page limit not enforced")
			}
			two, e := r.Properties(ctx, one[0].ID, "", 1)
			if e != nil {
				return e
			}
			if len(two) != 1 || two[0].ID <= one[0].ID {
				t.Fatal("keyset ordering/continuation failed")
			}
			empty, e := r.Properties(ctx, two[0].ID, "", 1)
			if e != nil {
				return e
			}
			if empty == nil || len(empty) != 0 {
				t.Fatal("terminal page not empty")
			}
			return marker
		})
		if err != marker {
			t.Fatal("keyset test failed")
		}
	})
	t.Run("completed and cancelled transaction safe errors", func(t *testing.T) {
		var retained *repository.Repository
		if err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error { retained = r; return nil }); err != nil {
			t.Fatal(err)
		}
		v, err := retained.Property(ctx, f.property)
		requireError(t, v, err, repository.ErrUnavailable)
		err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			v, e := r.Property(cancelled, f.property)
			requireError(t, v, e, repository.ErrUnavailable)
			return e
		})
		if err != repository.ErrUnavailable {
			t.Fatal(err)
		}
	})
}

func boundedContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
