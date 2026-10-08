package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type challengeFixture struct {
	db                *storage.RuntimeDB
	ctx               context.Context
	person, email, id uuid.UUID
	digest            [32]byte
}

func newChallengeFixture(t *testing.T) challengeFixture {
	t.Helper()
	f := challengeFixture{db: freshnessRuntimeDB(t), person: newID(t), email: newID(t), id: newID(t)}
	f.digest = sha256.Sum256([]byte(f.id.String()))
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	installation, application := newID(t), newID(t)
	err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(f.ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, f.person, installation, application); err != nil {
			return err
		}
		if _, err := tx.ExecContext(f.ctx, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key) VALUES($1,$2,$3,$4,$5,$5)`, f.email, f.person, installation, application, f.id.String()+"@example.test"); err != nil {
			return err
		}
		_, err := tx.ExecContext(f.ctx, `INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()-interval '1 hour',clock_timestamp()+interval '10 minutes')`, f.id, f.person, f.email, store.ChallengePasswordReset, f.digest[:])
		return err
	})
	if err != nil {
		t.Fatal("seed exact-owned challenge failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM identity_challenges WHERE id=$1`, f.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM identity_emails WHERE id=$1`, f.email); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM identity_persons WHERE id=$1`, f.person)
			return err
		}); err != nil {
			t.Error("exact-owned challenge cleanup failed")
		}
	})
	return f
}

func challengeRead(ctx context.Context, tx *sql.Tx, id uuid.UUID) (sql.NullTime, error) {
	var at sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT consumed_at FROM identity_challenges WHERE id=$1`, id).Scan(&at)
	return at, err
}
func challengeConsume(ctx context.Context, tx *sql.Tx, f challengeFixture) (store.ConsumedChallenge, error) {
	st, err := store.New(tx)
	if err != nil {
		return store.ConsumedChallenge{}, err
	}
	return st.ConsumeChallenge(ctx, f.id, store.ChallengePasswordReset, f.digest[:])
}
func challengeAssert(t *testing.T, f challengeFixture, got store.ConsumedChallenge, err, want error) {
	t.Helper()
	if err != want {
		t.Fatalf("consume error = %v; want %v", err, want)
	}
	if want != nil {
		if got != (store.ConsumedChallenge{}) {
			t.Fatal("error exposed partial output")
		}
		return
	}
	if got.PersonID != f.person || got.EmailID != f.email {
		t.Fatal("successful consume returned wrong binding")
	}
}

func TestChallengeFreshnessRequiredServiceWaits(t *testing.T) {
	for _, name := range []string{"expired", "live", "rollback", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newChallengeFixture(t)
			observation := freshnessFixture{ctx: f.ctx}
			waiterCtx, stop := context.WithCancel(f.ctx)
			defer stop()
			pidCh := make(chan int, 1)
			done := make(chan error, 1)
			joined := make(chan struct{})
			var got store.ConsumedChallenge
			var consumeErr error
			var during sql.NullTime
			var boundary, released time.Time
			rollback := errors.New("intentional challenge rollback")
			err := f.db.WithTx(f.ctx, nil, func(blocker *sql.Tx) error {
				query := `SELECT clock_timestamp()+interval '1 second' FROM identity_challenges WHERE id=$1 FOR UPDATE`
				if name == "expired" {
					query = `UPDATE identity_challenges SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING expires_at`
				}
				if err := blocker.QueryRowContext(f.ctx, query, f.id).Scan(&boundary); err != nil {
					return err
				}
				go func() {
					defer close(joined)
					done <- f.db.WithTx(waiterCtx, nil, func(tx *sql.Tx) error {
						var pid int
						if err := tx.QueryRowContext(waiterCtx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
							return err
						}
						pidCh <- pid
						got, consumeErr = challengeConsume(waiterCtx, tx, f)
						if consumeErr != nil {
							if name == "cancel" {
								return consumeErr
							}
							return nil
						}
						var err error
						during, err = challengeRead(waiterCtx, tx, f.id)
						if err != nil {
							return err
						}
						if name == "rollback" {
							return rollback
						}
						return nil
					})
				}()
				t.Cleanup(func() {
					stop()
					select {
					case <-joined:
					case <-time.After(5 * time.Second):
						t.Error("challenge waiter cleanup timed out")
					}
				})
				var pid int
				select {
				case pid = <-pidCh:
				case err := <-done:
					return err
				case <-f.ctx.Done():
					return f.ctx.Err()
				}
				freshnessObserveWait(t, observation, blocker, pid, boundary)
				released = freshnessCross(t, observation, blocker, boundary)
				if name == "cancel" {
					stop()
					select {
					case <-joined:
					case <-f.ctx.Done():
						return f.ctx.Err()
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal("challenge blocker failed")
			}
			select {
			case err = <-done:
			case <-f.ctx.Done():
				t.Fatal("challenge waiter did not finish")
			}
			want := error(nil)
			switch name {
			case "expired":
				want = store.ErrChallengeUnavailable
			case "cancel":
				want = store.ErrPersistence
			case "rollback":
				if !errors.Is(err, rollback) {
					t.Fatal("caller rollback not preserved")
				}
			}
			if name != "cancel" && name != "rollback" && err != nil {
				t.Fatal("challenge waiter transaction failed")
			}
			challengeAssert(t, f, got, consumeErr, want)
			if want == nil && (!during.Valid || during.Time.Before(released)) {
				t.Fatal("consumed timestamp precedes post-wait release")
			}
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
				after, err := challengeRead(f.ctx, tx, f.id)
				if err != nil {
					return err
				}
				if name == "live" {
					if !after.Valid || !after.Time.Equal(during.Time) {
						t.Fatal("committed consume timestamp differs")
					}
					got, err := challengeConsume(f.ctx, tx, f)
					challengeAssert(t, f, got, err, store.ErrChallengeUnavailable)
				} else {
					if after.Valid {
						t.Fatal("denied or rolled back consume persisted")
					}
					if name == "rollback" {
						got, err := challengeConsume(f.ctx, tx, f)
						challengeAssert(t, f, got, err, nil)
					}
				}
				return nil
			}); err != nil {
				t.Fatal("verify challenge persistence failed")
			}
		})
	}
}

func TestChallengeFreshnessRequiredServiceDenials(t *testing.T) {
	for _, name := range []string{"purpose", "digest", "unknown", "consumed", "expired", "repeatable_read", "serializable", "read_uncommitted", "read_only", "completed", "canceled", "invalid_id", "invalid_version", "invalid_purpose", "short_digest", "long_digest", "nil_context", "nil_receiver", "zero_store"} {
		t.Run(name, func(t *testing.T) {
			f := newChallengeFixture(t)
			options := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
			switch name {
			case "repeatable_read":
				options.Isolation = sql.LevelRepeatableRead
			case "serializable":
				options.Isolation = sql.LevelSerializable
			case "read_uncommitted":
				options.Isolation = sql.LevelReadUncommitted
			case "read_only":
				options.ReadOnly = true
			}
			rollback := errors.New("rollback challenge denial")
			err := f.db.WithTx(f.ctx, options, func(tx *sql.Tx) error {
				st, err := store.New(tx)
				if err != nil {
					return err
				}
				id, purpose, digest, ctx := f.id, store.ChallengePasswordReset, f.digest[:], f.ctx
				want := store.ErrChallengeUnavailable
				switch name {
				case "purpose":
					purpose = store.ChallengeEmailMagicLink
				case "digest":
					d := sha256.Sum256([]byte("wrong"))
					digest = d[:]
				case "unknown":
					id = newID(t)
				case "consumed":
					if _, err := tx.ExecContext(ctx, `UPDATE identity_challenges SET consumed_at=clock_timestamp() WHERE id=$1`, id); err != nil {
						return err
					}
				case "expired":
					if _, err := tx.ExecContext(ctx, `UPDATE identity_challenges SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, id); err != nil {
						return err
					}
				case "repeatable_read", "serializable", "read_uncommitted", "read_only":
					want = store.ErrPersistence
				case "completed":
					if err := tx.Rollback(); err != nil {
						return err
					}
					want = store.ErrPersistence
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					want = store.ErrPersistence
				case "invalid_id":
					id = uuid.Nil
					want = store.ErrInvalidInput
				case "invalid_version":
					id = uuid.New()
					want = store.ErrInvalidInput
				case "invalid_purpose":
					purpose = "invalid"
					want = store.ErrInvalidInput
				case "short_digest":
					digest = digest[:31]
					want = store.ErrInvalidInput
				case "long_digest":
					digest = make([]byte, 33)
					want = store.ErrInvalidInput
				case "nil_context":
					ctx = nil
					want = store.ErrInvalidInput
				case "nil_receiver":
					st = nil
					want = store.ErrInvalidInput
				case "zero_store":
					st = &store.Store{}
					want = store.ErrInvalidInput
				}
				// A completed transaction makes any attempted SQL fail. Invalid shapes must
				// still return ErrInvalidInput before touching it.
				if want == store.ErrInvalidInput {
					if err := tx.Rollback(); err != nil {
						return err
					}
				}
				got, err := st.ConsumeChallenge(ctx, id, purpose, digest)
				challengeAssert(t, f, got, err, want)
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("challenge denial schedule failed")
			}
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
				at, err := challengeRead(f.ctx, tx, f.id)
				if at.Valid {
					t.Fatal("denial persisted consumption")
				}
				return err
			}); err != nil {
				t.Fatal("read challenge after denial failed")
			}
		})
	}
}

func TestChallengeFreshnessRequiredServiceConcurrent(t *testing.T) {
	f := newChallengeFixture(t)
	pidCh := make(chan int, 1)
	done := make(chan error, 1)
	joined := make(chan struct{})
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var second store.ConsumedChallenge
	var secondErr error
	err := f.db.WithTx(f.ctx, nil, func(first *sql.Tx) error {
		got, err := challengeConsume(f.ctx, first, f)
		challengeAssert(t, f, got, err, nil)
		var boundary time.Time
		if err := first.QueryRowContext(f.ctx, `SELECT clock_timestamp()+interval '5 seconds'`).Scan(&boundary); err != nil {
			return err
		}
		go func() {
			defer close(joined)
			done <- f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var pid int
				if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				pidCh <- pid
				second, secondErr = challengeConsume(ctx, tx, f)
				return nil
			})
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-joined:
			case <-time.After(5 * time.Second):
				t.Error("competing consumer cleanup timed out")
			}
		})
		var pid int
		select {
		case pid = <-pidCh:
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
		freshnessObserveWait(t, freshnessFixture{ctx: f.ctx}, first, pid, boundary)
		return nil
	})
	if err != nil {
		t.Fatal("first concurrent consume failed")
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal("second consumer did not finish")
	}
	if err != nil {
		t.Fatal("second transaction failed")
	}
	challengeAssert(t, f, second, secondErr, store.ErrChallengeUnavailable)
	if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		at, err := challengeRead(f.ctx, tx, f.id)
		if !at.Valid {
			t.Fatal("successful concurrent consume not committed")
		}
		return err
	}); err != nil {
		t.Fatal("read concurrent result failed")
	}
}

// This real-transaction forwarding probe sets the exact-owned expiry to the
// actual sampled database instant. It supplements the unmodified-clock waits.
func TestChallengeFreshnessRequiredServiceExactBoundary(t *testing.T) {
	f := newChallengeFixture(t)
	rollback := errors.New("rollback exact challenge boundary")
	err := f.db.WithTx(f.ctx, nil, func(actual *sql.Tx) error {
		connector := &freshnessBoundaryConnector{tx: actual, boundary: func(ctx context.Context, sampled time.Time) error {
			_, err := actual.ExecContext(ctx, `UPDATE identity_challenges SET expires_at=$2 WHERE id=$1`, f.id, sampled)
			return err
		}}
		probeDB := sql.OpenDB(connector)
		defer func() {
			if err := probeDB.Close(); err != nil {
				t.Error("challenge boundary facade close failed")
			}
		}()
		probeTx, err := probeDB.BeginTx(f.ctx, nil)
		if err != nil {
			return err
		}
		defer func() {
			if err := probeTx.Rollback(); err != nil {
				t.Error("challenge boundary facade rollback failed")
			}
		}()
		got, err := challengeConsume(f.ctx, probeTx, f)
		challengeAssert(t, f, got, err, store.ErrChallengeUnavailable)
		at, err := challengeRead(f.ctx, actual, f.id)
		if err != nil {
			return err
		}
		if at.Valid {
			t.Fatal("exact expiry consumed challenge")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("exact challenge boundary schedule failed")
	}
}

// Errors from real runtime statements at both post-lock stages must remain
// sanitized and return no bound identity. The caller retains rollback control.
func TestChallengeFreshnessRequiredServiceStatementErrors(t *testing.T) {
	for _, name := range []string{"clock", "update"} {
		t.Run(name, func(t *testing.T) {
			f := newChallengeFixture(t)
			rollback := errors.New("rollback challenge statement error")
			err := f.db.WithTx(f.ctx, nil, func(actual *sql.Tx) error {
				connector := &freshnessBoundaryConnector{tx: actual, boundary: func(ctx context.Context, _ time.Time) error {
					_, err := actual.ExecContext(ctx, `SELECT 1/0`)
					if err == nil {
						t.Fatal("database error probe did not fail")
					}
					if name == "clock" {
						return err
					}
					// The real transaction is now aborted; the actual method's final UPDATE
					// must fail even though its clock scan was successfully forwarded.
					return nil
				}}
				probeDB := sql.OpenDB(connector)
				defer func() {
					if err := probeDB.Close(); err != nil {
						t.Error("error facade close failed")
					}
				}()
				probeTx, err := probeDB.BeginTx(f.ctx, nil)
				if err != nil {
					return err
				}
				defer func() {
					if err := probeTx.Rollback(); err != nil {
						t.Error("error facade rollback failed")
					}
				}()
				got, err := challengeConsume(f.ctx, probeTx, f)
				challengeAssert(t, f, got, err, store.ErrPersistence)
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("statement failure rollback failed")
			}
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
				at, err := challengeRead(f.ctx, tx, f.id)
				if at.Valid {
					t.Fatal("failed statement consumed challenge")
				}
				return err
			}); err != nil {
				t.Fatal("read after statement error failed")
			}
		})
	}
}
