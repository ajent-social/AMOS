package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/repository"
)

// invocationSQL is only a real transaction signature adapter. It is not the
// operation executor's guarded DBTX and does not qualify callback invalidation.
type invocationSQL struct{ tx *sql.Tx }

func (d invocationSQL) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return d.tx.ExecContext(ctx, q, a...)
}
func (d invocationSQL) QueryContext(ctx context.Context, q string, a ...any) (operation.Rows, error) {
	return d.tx.QueryContext(ctx, q, a...)
}
func (d invocationSQL) QueryRowContext(ctx context.Context, q string, a ...any) operation.Row {
	return d.tx.QueryRowContext(ctx, q, a...)
}

func TestInvocationRepositoryRequiredService(t *testing.T) {
	db := continuityRuntimeDB(t)
	for _, useDBTX := range []bool{false, true} {
		name := "legacy"
		if useDBTX {
			name = "DBTX"
		}
		t.Run(name, func(t *testing.T) {
			f := seed(t, db)
			ctx := boundedContext(t)
			call := func(fn func(*repository.Repository) error) error {
				return db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
					var r *repository.Repository
					var err error
					if useDBTX {
						r, err = repository.NewWithDBTX(invocationSQL{tx}, f.scope)
					} else {
						r, err = repository.New(tx, f.scope)
					}
					if err != nil {
						return err
					}
					return fn(r)
				})
			}
			t.Run("ordinary read and committed revision activity draft", func(t *testing.T) {
				err := call(func(r *repository.Repository) error {
					p, err := r.Properties(ctx, "", "%_", 10)
					if err != nil || len(p) != 1 || p[0].ID != f.property {
						return errors.New("scoped literal discovery differs")
					}
					c, err := r.ChangeCase(ctx, f.caseID, 1, domain.InProgress, f.actor)
					if err != nil || c.Revision != 2 {
						return errors.New("case revision write differs")
					}
					draft, err := r.SaveDraft(ctx, f.caseID, 2, "Unsent adapter draft", f.actor)
					if err != nil || draft.CaseRevision != 2 {
						return errors.New("draft write differs")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
					c, err := r.Case(ctx, f.caseID)
					if err != nil || c.Revision != 2 || c.Status != domain.InProgress {
						return errors.New("committed case absent")
					}
					d, err := r.Draft(ctx, f.caseID)
					if err != nil || d.CaseRevision != 2 || d.Body != "Unsent adapter draft" {
						return errors.New("committed draft absent")
					}
					a, err := r.Activity(ctx, "", 10)
					if err != nil || len(a) != 2 {
						return errors.New("committed activity differs")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
			t.Run("caller rollback preserves all records", func(t *testing.T) {
				rollback := errors.New("intentional caller rollback")
				err := call(func(r *repository.Repository) error {
					if _, err := r.ChangeCase(ctx, f.caseID, 2, domain.Completed, f.actor); err != nil {
						return err
					}
					if _, err := r.SaveDraft(ctx, f.caseID, 3, "Never committed", f.actor); err != nil {
						return err
					}
					return rollback
				})
				if !errors.Is(err, rollback) {
					t.Fatal("caller rollback sentinel lost")
				}
				err = transact(db, f.scope, func(ctx context.Context, r *repository.Repository) error {
					c, err := r.Case(ctx, f.caseID)
					if err != nil || c.Revision != 2 || c.Status != domain.InProgress {
						return errors.New("caller rollback lost case")
					}
					d, err := r.Draft(ctx, f.caseID)
					if err != nil || d.Body != "Unsent adapter draft" || d.CaseRevision != 2 {
						return errors.New("caller rollback lost draft")
					}
					a, err := r.Activity(ctx, "", 10)
					if err != nil || len(a) != 2 {
						return errors.New("caller rollback retained provisional activity")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
