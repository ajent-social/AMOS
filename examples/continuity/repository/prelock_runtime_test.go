package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/repository"
)

// These tests require the real operator-owned TLS fixture. They do not qualify
// Root admission, native authority, policy, billing, or operation dispatch.
func TestPrelockRequiredServiceMutations(t *testing.T) {
	db := continuityRuntimeDB(t)
	for _, action := range []string{"case.changed", "checklist.changed", "procedure.changed", "draft.saved"} {
		t.Run(action, func(t *testing.T) {
			f := seed(t, db)
			id := f.caseID
			if action == "checklist.changed" {
				id = f.application
			}
			if action == "procedure.changed" {
				id = f.procedure
			}
			for _, commit := range []bool{false, true} {
				rollback := errors.New("intentional rollback")
				err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
					locked, e := r.PrelockMutation(ctx, action, id)
					if e != nil {
						return e
					}
					if len(locked) == 0 {
						return errors.New("empty closure")
					}
					switch action {
					case "case.changed":
						_, e = r.ChangeCase(ctx, id, 1, domain.Completed, f.actor)
					case "checklist.changed":
						_, e = r.SetChecklist(ctx, id, 1, f.support, true, f.actor)
					case "procedure.changed":
						_, e = r.EditProcedure(ctx, id, 1, "Updated plain text", f.actor)
					case "draft.saved":
						_, e = r.SaveDraft(ctx, id, 1, "Draft plain text", f.actor)
					}
					if e != nil {
						return e
					}
					if !commit {
						return rollback
					}
					return nil
				})
				if (!commit && !errors.Is(err, rollback)) || (commit && err != nil) {
					t.Fatalf("mutation transaction: %v", err)
				}
			}
			err := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
				// Existing-draft coverage uses a new transaction, preserving once-per-call
				// custody rather than treating a returned list as a reusable capability.
				locked, e := r.PrelockMutation(ctx, action, id)
				if e != nil {
					return e
				}
				if action == "draft.saved" {
					found := false
					for _, v := range locked {
						if v.Kind == "draft" && v.ID == id {
							found = true
						}
					}
					if !found {
						return errors.New("draft key absent from closure")
					}
				}
				entries, e := r.Activity(ctx, "", 10)
				if e != nil {
					return e
				}
				if len(entries) != 1 {
					return errors.New("rollback activity persisted or commit activity missing")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPrelockRequiredServiceModesAndScope(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	ctx := boundedContext(t)
	for _, options := range []*sql.TxOptions{{Isolation: sql.LevelRepeatableRead}, {Isolation: sql.LevelSerializable}, {Isolation: sql.LevelReadCommitted, ReadOnly: true}} {
		err := db.WithTx(ctx, options, func(tx *sql.Tx) error {
			r, e := repository.New(tx, f.scope)
			if e != nil {
				return e
			}
			got, e := r.PrelockMutation(ctx, "case.changed", f.caseID)
			if got != nil || e != repository.ErrUnavailable {
				return errors.New("unsupported transaction mode admitted")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	foreign := freshScope(t)
	if err := transact(db, foreign, func(ctx context.Context, r *repository.Repository) error {
		got, e := r.PrelockMutation(ctx, "case.changed", f.caseID)
		if got != nil || e != repository.ErrNotFound {
			return errors.New("foreign scope admitted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var retained *repository.Repository
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { var e error; retained, e = repository.New(tx, f.scope); return e }); err != nil {
		t.Fatal(err)
	}
	if got, e := retained.PrelockMutation(ctx, "case.changed", f.caseID); got != nil || e != repository.ErrUnavailable {
		t.Fatal("closed transaction admitted")
	}
}

// The first transaction owns the discovered source. The second must be visibly
// blocked on that row before the first changes the case relation. No timing
// assumption substitutes for observing the actual PostgreSQL wait edge.
func TestPrelockRequiredServiceChangedClosure(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	changed := make(chan error, 1)
	err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(first *sql.Tx) error {
		var firstPID int
		if e := first.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&firstPID); e != nil {
			return e
		}
		var source string
		if e := first.QueryRowContext(ctx, `SELECT id FROM public.continuity_sources WHERE `+scopeSQL+` AND id=$5 FOR UPDATE`, args(f.scope, f.source)...).Scan(&source); e != nil {
			return e
		}
		secondPID := make(chan int, 1)
		go func() {
			changed <- db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(second *sql.Tx) error {
				var pid int
				if e := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
					return e
				}
				secondPID <- pid
				r, e := repository.New(second, f.scope)
				if e != nil {
					return e
				}
				got, e := r.PrelockMutation(ctx, "case.changed", f.caseID)
				if got != nil || e != repository.ErrConflict {
					return errors.New("changed closure did not fail closed")
				}
				return nil
			})
		}()
		var pid int
		select {
		case pid = <-secondPID:
		case e := <-changed:
			return e
		case <-ctx.Done():
			return ctx.Err()
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if e := first.QueryRowContext(ctx, `SELECT $1=ANY(pg_catalog.pg_blocking_pids($2))`, firstPID, pid).Scan(&blocked); e != nil {
				return e
			}
			if blocked {
				break
			}
			select {
			case e := <-changed:
				if e == nil {
					return errors.New("prelock completed without blocking")
				}
				return e
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		// Sources have no foreign key. A changed relation must fail equality
		// before querying or locking that newly observed resource.
		raw, e := json.Marshal([]string{freshID(t)})
		if e != nil {
			return e
		}
		_, e = first.ExecContext(ctx, `UPDATE public.continuity_cases SET source_ids=$6,revision=revision+1 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.caseID, raw)...)
		return e
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	select {
	case err = <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("prelock did not finish after observed blocker committed")
	}
}

func TestPrelockRequiredServiceMissingRelation(t *testing.T) {
	db := continuityRuntimeDB(t)
	f := seed(t, db)
	missing := freshID(t)
	ctx := boundedContext(t)
	raw, err := json.Marshal([]string{missing})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE public.continuity_cases SET source_ids=$6 WHERE `+scopeSQL+` AND id=$5`, args(f.scope, f.caseID, raw)...)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
		got, e := r.PrelockMutation(ctx, "draft.saved", f.caseID)
		if !reflect.DeepEqual(got, []repository.LockedResource(nil)) || e != repository.ErrNotFound {
			return errors.New("missing relation admitted")
		}
		return e
	}); err != repository.ErrNotFound {
		t.Fatal("missing relation did not roll transaction back")
	}
}

func TestPrelockRequiredServiceDraftParentWait(t *testing.T) {
	db := continuityRuntimeDB(t)
	for _, exists := range []bool{false, true} {
		for _, cancelWait := range []bool{false, true} {
			name := "absent"
			if exists {
				name = "existing"
			}
			if cancelWait {
				name += "/cancel"
			}
			t.Run(name, func(t *testing.T) {
				f := seed(t, db)
				if exists {
					if e := transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
						_, e := r.PrelockMutation(ctx, "draft.saved", f.caseID)
						if e != nil {
							return e
						}
						_, e = r.SaveDraft(ctx, f.caseID, 1, "Existing draft", f.actor)
						return e
					}); e != nil {
						t.Fatal(e)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				waitingCtx, cancelWaiting := context.WithCancel(ctx)
				defer cancelWaiting()
				result := make(chan error, 1)
				started := false
				err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(first *sql.Tx) error {
					r, e := repository.New(first, f.scope)
					if e != nil {
						return e
					}
					if _, e = r.PrelockMutation(ctx, "draft.saved", f.caseID); e != nil {
						return e
					}
					var firstPID int
					if e = first.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&firstPID); e != nil {
						return e
					}
					pidCh := make(chan int, 1)
					started = true
					go func() {
						result <- db.WithTx(waitingCtx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(second *sql.Tx) error {
							var pid int
							if e := second.QueryRowContext(waitingCtx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
								return e
							}
							pidCh <- pid
							r, e := repository.New(second, f.scope)
							if e != nil {
								return e
							}
							v, e := r.PrelockMutation(waitingCtx, "draft.saved", f.caseID)
							if cancelWait {
								if v != nil || e != repository.ErrUnavailable {
									return errors.New("canceled wait disclosed output")
								}
								return e
							}
							if e != nil {
								return e
							}
							draft, e := r.Draft(waitingCtx, f.caseID)
							if e != nil {
								return e
							}
							if draft.Body != "Committed after wait" {
								return errors.New("draft parent did not serialize writer")
							}
							return nil
						})
					}()
					var pid int
					select {
					case pid = <-pidCh:
					case <-ctx.Done():
						return ctx.Err()
					}
					ticker := time.NewTicker(5 * time.Millisecond)
					defer ticker.Stop()
					for {
						var blocked bool
						if e = first.QueryRowContext(ctx, `SELECT $1=ANY(pg_catalog.pg_blocking_pids($2))`, firstPID, pid).Scan(&blocked); e != nil {
							return e
						}
						if blocked {
							break
						}
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-ticker.C:
						}
					}
					if cancelWait {
						cancelWaiting()
						return nil
					}
					_, e = r.SaveDraft(ctx, f.caseID, 1, "Committed after wait", f.actor)
					return e
				})
				if err != nil {
					cancel()
				}
				if started {
					select {
					case e := <-result:
						if err == nil {
							if cancelWait {
								if !errors.Is(e, repository.ErrUnavailable) {
									t.Errorf("canceled result: %v", e)
								}
							} else if e != nil {
								t.Error(e)
							}
						}
					case <-time.After(12 * time.Second):
						t.Error("waiting transaction did not drain")
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
