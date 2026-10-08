package email

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type confirmationFixture struct {
	db                              *storage.RuntimeDB
	ctx                             context.Context
	cfg                             Config
	person, email, other, challenge uuid.UUID
	token                           string
}

type confirmationState struct {
	person, email, other, challenge string
}

func newConfirmationFixture(t *testing.T) confirmationFixture {
	t.Helper()
	f := confirmationFixture{db: emailRuntimeDB(t), cfg: emailTxConfig(t), person: emailID(t), email: emailID(t), other: emailID(t), challenge: emailID(t)}
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	token, digest, err := newToken()
	if err != nil {
		t.Fatal("generate synthetic confirmation token failed")
	}
	f.token = token
	if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(f.ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'pending_verification')`, f.person, f.cfg.InstallationID, f.cfg.ApplicationID); err != nil {
			return err
		}
		for _, id := range []uuid.UUID{f.email, f.other} {
			if _, err := tx.ExecContext(f.ctx, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key) VALUES($1,$2,$3,$4,$5,$5)`, id, f.person, f.cfg.InstallationID, f.cfg.ApplicationID, id.String()+"@example.test"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(f.ctx, `INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,created_at,expires_at) VALUES($1,$2,$3,'email_verification',$4,clock_timestamp()-interval '1 hour',clock_timestamp()+interval '10 minutes')`, f.challenge, f.person, f.email, digest[:])
		return err
	}); err != nil {
		t.Fatal("seed exact-owned confirmation failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM identity_challenges WHERE id=$1`, f.challenge); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM identity_emails WHERE id IN ($1,$2)`, f.email, f.other); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM identity_persons WHERE id=$1`, f.person)
			return err
		}); err != nil {
			t.Error("exact-owned confirmation cleanup failed")
		}
	})
	return f
}

func (f confirmationFixture) service(t *testing.T, runner TxRunner) *Service {
	t.Helper()
	s, err := NewWithTxRunner(runner, nil, nil, nil, f.cfg)
	if err != nil {
		t.Fatal("construct confirmation service failed")
	}
	return s
}
func (f confirmationFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error { _, err := tx.ExecContext(f.ctx, q, args...); return err }); err != nil {
		t.Fatal("confirmation fixture DML failed")
	}
}
func (f confirmationFixture) state(t *testing.T) confirmationState {
	t.Helper()
	var s confirmationState
	if err := f.db.WithTx(f.ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(f.ctx, `SELECT row_to_json(p)::text,row_to_json(e)::text,row_to_json(o)::text,row_to_json(c)::text FROM identity_persons p,identity_emails e,identity_emails o,identity_challenges c WHERE p.id=$1 AND e.id=$2 AND o.id=$3 AND c.id=$4`, f.person, f.email, f.other, f.challenge).Scan(&s.person, &s.email, &s.other, &s.challenge)
	}); err != nil {
		t.Fatal("read full confirmation state failed")
	}
	return s
}
func (f confirmationFixture) unchanged(t *testing.T, before confirmationState) {
	t.Helper()
	if f.state(t) != before {
		t.Fatal("failed confirmation changed persistent rows")
	}
}
func (f confirmationFixture) committed(t *testing.T, earliest time.Time) {
	t.Helper()
	var active, verified, other bool
	var consumed sql.NullTime
	if err := f.db.WithTx(f.ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(f.ctx, `SELECT p.state='active',e.verified_at IS NOT NULL,o.verified_at IS NOT NULL,c.consumed_at FROM identity_persons p,identity_emails e,identity_emails o,identity_challenges c WHERE p.id=$1 AND e.id=$2 AND o.id=$3 AND c.id=$4`, f.person, f.email, f.other, f.challenge).Scan(&active, &verified, &other, &consumed)
	}); err != nil {
		t.Fatal("read committed confirmation failed")
	}
	if !active || !verified || other || !consumed.Valid || consumed.Time.Before(earliest) {
		t.Fatal("confirmation commit state or post-wait timestamp incorrect")
	}
}

// Observe from the holder connection so the two-connection runtime pool never
// needs a third connection to establish exact backend blocking and timing.
func confirmationWait(t *testing.T, ctx context.Context, holder *sql.Tx, pid int, boundary time.Time) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := holder.ExecContext(ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
			t.Fatal("clear waiter snapshot failed")
		}
		var waiting sql.NullBool
		var start, statement sql.NullTime
		if err := holder.QueryRowContext(ctx, `SELECT wait_event_type='Lock' AND pg_backend_pid()=ANY(pg_blocking_pids(pid)),xact_start,query_start FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting, &start, &statement); err != nil {
			t.Fatal("observe exact confirmation waiter failed")
		}
		if waiting.Valid && waiting.Bool {
			if !start.Valid || !statement.Valid || !start.Time.Before(boundary) || !statement.Time.Before(boundary) {
				t.Fatal("confirmation wait starts did not precede expiry")
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact confirmation blocker not observed")
		case <-ticker.C:
		}
	}
}
func confirmationClock(t *testing.T, ctx context.Context, holder *sql.Tx, boundary time.Time) time.Time {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var now time.Time
		if err := holder.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal("observe confirmation database clock failed")
		}
		if !now.Before(boundary) {
			return now
		}
		select {
		case <-ctx.Done():
			t.Fatal("confirmation expiry boundary not crossed")
		case <-ticker.C:
		}
	}
}

func TestConfirmationFreshnessRequiredServiceWaits(t *testing.T) {
	for _, row := range []string{"challenge", "email", "person"} {
		for _, mode := range []string{"expired", "live", "cancel", "statement"} {
			t.Run(row+"/"+mode, func(t *testing.T) {
				f := newConfirmationFixture(t)
				var boundary time.Time
				// Commit the expiry before taking a downstream row lock. The email/person
				// schedules must not accidentally block on a holder-owned challenge first.
				if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
					lifetime := "10 minutes"
					if mode == "expired" {
						lifetime = "2 seconds"
					}
					return tx.QueryRowContext(f.ctx, `UPDATE identity_challenges SET expires_at=clock_timestamp()+$2::interval WHERE id=$1 RETURNING expires_at`, f.challenge, lifetime).Scan(&boundary)
				}); err != nil {
					t.Fatal("set confirmation boundary failed")
				}
				before := f.state(t)
				waiterCtx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				pidCh := make(chan int, 1)
				done := make(chan error, 1)
				joined := make(chan struct{})
				runner := emailRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
					return f.db.WithTx(ctx, opts, func(tx *sql.Tx) error {
						var pid int
						if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
							return err
						}
						if mode == "statement" {
							if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout='500ms'`); err != nil {
								return err
							}
						}
						pidCh <- pid
						return fn(tx)
					})
				})
				service := f.service(t, runner)
				var release time.Time
				err := f.db.WithTx(f.ctx, nil, func(holder *sql.Tx) error {
					q := `SELECT id FROM identity_challenges WHERE id=$1 FOR UPDATE`
					id := f.challenge
					if row == "email" {
						q = `SELECT id FROM identity_emails WHERE id=$1 FOR UPDATE`
						id = f.email
					}
					if row == "person" {
						q = `SELECT id FROM identity_persons WHERE id=$1 FOR UPDATE`
						id = f.person
					}
					var locked uuid.UUID
					if err := holder.QueryRowContext(f.ctx, q, id).Scan(&locked); err != nil {
						return err
					}
					go func() { defer close(joined); done <- service.Confirm(waiterCtx, f.challenge, f.token) }()
					t.Cleanup(func() {
						cancel()
						select {
						case <-joined:
						case <-time.After(5 * time.Second):
							t.Error("confirmation waiter cleanup timed out")
						}
					})
					var pid int
					select {
					case pid = <-pidCh:
					case <-f.ctx.Done():
						return f.ctx.Err()
					}
					confirmationWait(t, f.ctx, holder, pid, boundary)
					if mode == "expired" {
						release = confirmationClock(t, f.ctx, holder, boundary)
					} else {
						release = confirmationClock(t, f.ctx, holder, time.Time{})
					}
					if mode == "cancel" {
						cancel()
					}
					if mode == "cancel" || mode == "statement" {
						select {
						case <-joined:
						case <-f.ctx.Done():
							return f.ctx.Err()
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal("confirmation holder transaction failed")
				}
				var got error
				select {
				case got = <-done:
				case <-f.ctx.Done():
					t.Fatal("confirmation waiter did not complete")
				}
				want := error(nil)
				if mode == "expired" {
					want = ErrChallengeUnavailable
				}
				if mode == "cancel" || mode == "statement" {
					want = ErrUnavailable
				}
				if got != want {
					t.Fatalf("post-wait confirmation = %v; want %v", got, want)
				}
				if want != nil {
					f.unchanged(t, before)
					return
				}
				f.committed(t, release)
				if f.state(t).other != before.other {
					t.Fatal("confirmation changed other contact")
				}
				if err := service.Confirm(f.ctx, f.challenge, f.token); err != ErrChallengeUnavailable {
					t.Fatal("committed confirmation replay accepted")
				}
			})
		}
	}
}

func TestConfirmationFreshnessRequiredServiceDenials(t *testing.T) {
	for _, name := range []string{"purpose", "digest", "installation", "application", "unknown", "expired", "consumed", "verified", "active", "disabled", "malformed"} {
		t.Run(name, func(t *testing.T) {
			f := newConfirmationFixture(t)
			id, token := f.challenge, f.token
			switch name {
			case "purpose":
				f.exec(t, `UPDATE identity_challenges SET purpose='password_reset' WHERE id=$1`, f.challenge)
			case "digest":
				var err error
				token, _, err = newToken()
				if err != nil {
					t.Fatal("generate wrong token failed")
				}
			case "installation":
				f.cfg.InstallationID = emailID(t)
			case "application":
				f.cfg.ApplicationID = emailID(t)
			case "unknown":
				id = emailID(t)
			case "expired":
				f.exec(t, `UPDATE identity_challenges SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.challenge)
			case "consumed":
				f.exec(t, `UPDATE identity_challenges SET consumed_at=clock_timestamp() WHERE id=$1`, f.challenge)
			case "verified":
				f.exec(t, `UPDATE identity_emails SET verified_at=clock_timestamp() WHERE id=$1`, f.email)
			case "active":
				f.exec(t, `UPDATE identity_persons SET state='active' WHERE id=$1`, f.person)
			case "disabled":
				f.exec(t, `UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, f.person)
			case "malformed":
				token = "invalid"
			}
			before := f.state(t)
			if err := f.service(t, f.db).Confirm(f.ctx, id, token); err != ErrChallengeUnavailable {
				t.Fatal("unavailable confirmation did not retain non-enumerating sentinel")
			}
			f.unchanged(t, before)
		})
	}
}

func TestConfirmationFreshnessRequiredServiceCompletion(t *testing.T) {
	for _, mode := range []string{"rollback", "commit", "isolation", "readonly"} {
		t.Run(mode, func(t *testing.T) {
			f := newConfirmationFixture(t)
			before := f.state(t)
			forced := errors.New("intentional caller rollback")
			reached := false
			var runnerErr error
			runner := emailRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
				if opts == nil || opts.Isolation != sql.LevelReadCommitted {
					return errors.New("explicit READ COMMITTED absent")
				}
				// These two cases deliberately forward altered options only in the test
				// adapter to prove failures remain unavailable; production forwards exactly.
				forwarded := *opts
				if mode == "isolation" {
					forwarded.Isolation = sql.LevelRepeatableRead
				}
				if mode == "readonly" {
					forwarded.ReadOnly = true
				}
				runnerErr = f.db.WithTx(ctx, &forwarded, func(tx *sql.Tx) error {
					if err := fn(tx); err != nil {
						return err
					}
					var written bool
					if err := tx.QueryRowContext(ctx, `SELECT p.state='active' AND e.verified_at IS NOT NULL AND c.consumed_at IS NOT NULL FROM identity_persons p,identity_emails e,identity_challenges c WHERE p.id=$1 AND e.id=$2 AND c.id=$3`, f.person, f.email, f.challenge).Scan(&written); err != nil {
						return err
					}
					if !written {
						return errors.New("callback did not complete all three writes")
					}
					reached = true
					if mode == "rollback" {
						return forced
					}
					// Actual RuntimeDB subsequently attempts Commit on an ended transaction.
					// This is bounded completion-error injection, not unknown-outcome proof.
					return tx.Rollback()
				})
				return runnerErr
			})
			if err := f.service(t, runner).Confirm(f.ctx, f.challenge, f.token); err != ErrUnavailable {
				t.Fatal("runner failure reported confirmation success")
			}
			if mode == "rollback" && (!reached || !errors.Is(runnerErr, forced)) {
				t.Fatal("caller rollback branch not reached")
			}
			if mode == "commit" && (!reached || runnerErr != storage.ErrTransaction) {
				t.Fatal("actual runtime commit failure branch not reached")
			}
			f.unchanged(t, before)
			if err := f.service(t, f.db).Confirm(f.ctx, f.challenge, f.token); err != nil {
				t.Fatal("live confirmation failed after rollback")
			}
			f.committed(t, time.Time{})
		})
	}
}

func TestConfirmationFreshnessRequiredServiceConcurrent(t *testing.T) {
	f := newConfirmationFixture(t)
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	done := make(chan error, 2)
	runner := emailRunnerFunc(func(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
		return f.db.WithTx(ctx, opts, func(tx *sql.Tx) error {
			ready <- struct{}{}
			select {
			case <-start:
			case <-ctx.Done():
				return ctx.Err()
			}
			return fn(tx)
		})
	})
	s := f.service(t, runner)
	for range 2 {
		go func() { done <- s.Confirm(f.ctx, f.challenge, f.token) }()
	}
	for range 2 {
		select {
		case <-ready:
		case <-f.ctx.Done():
			t.Fatal("concurrent confirmations did not start")
		}
	}
	close(start)
	successes, denials := 0, 0
	for range 2 {
		select {
		case err := <-done:
			switch err {
			case nil:
				successes++
			case ErrChallengeUnavailable:
				denials++
			default:
				t.Fatal("concurrent confirmation persistence failed")
			}
		case <-f.ctx.Done():
			t.Fatal("concurrent confirmations did not finish")
		}
	}
	if successes != 1 || denials != 1 {
		t.Fatal("concurrent confirmations did not produce exactly one committed success")
	}
	f.committed(t, time.Time{})
}
