package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// Required operator-owned TLS runtime profile; no DDL or admin connection.
func freshnessRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_SERVICES_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_SERVICES_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
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

type freshnessFixture struct {
	db              *storage.RuntimeDB
	ctx             context.Context
	person, session uuid.UUID
	scope           store.SessionScope
	digest          [32]byte
}

func newFreshnessFixture(t *testing.T) freshnessFixture {
	t.Helper()
	f := freshnessFixture{db: freshnessRuntimeDB(t), person: newID(t), session: newID(t), scope: store.SessionScope{InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t)}}
	f.digest = sha256.Sum256([]byte(f.session.String()))
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(f.ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, f.person, f.scope.InstallationID, f.scope.ApplicationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(f.ctx, `INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,authenticated_at,issued_at,last_seen_at,idle_expires_at,expires_at,assurance_level,assurance_expires_at)
  VALUES($1,$2,$3,$4,$5,$6,0,'email_password',clock_timestamp()-interval '1 minute',clock_timestamp()-interval '1 minute',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '10 minutes',clock_timestamp()+interval '20 minutes','aal2',clock_timestamp()+interval '5 minutes')`, f.session, f.person, f.scope.InstallationID, f.scope.ApplicationID, f.scope.EnvironmentID, f.digest[:])
		return err
	})
	if err != nil {
		t.Fatal("seed exact-owned runtime session failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM identity_sessions WHERE id=$1`, f.session); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM identity_persons WHERE id=$1`, f.person)
			return err
		}); err != nil {
			t.Error("exact-owned freshness cleanup failed")
		}
	})
	return f
}

type freshnessTimes struct{ seen, idle, absolute time.Time }

func freshnessRead(ctx context.Context, tx *sql.Tx, id uuid.UUID) (freshnessTimes, error) {
	var v freshnessTimes
	err := tx.QueryRowContext(ctx, `SELECT last_seen_at,idle_expires_at,expires_at FROM identity_sessions WHERE id=$1`, id).Scan(&v.seen, &v.idle, &v.absolute)
	return v, err
}
func freshnessSame(a, b freshnessTimes) bool {
	return a.seen.Equal(b.seen) && a.idle.Equal(b.idle) && a.absolute.Equal(b.absolute)
}

// The blocker itself observes the exact backend waiting on its transaction.
// Both PostgreSQL transaction and active lock-statement start must precede the
// expiry, catching transaction-clock and pre-lock statement/CTE-clock mutants.
func freshnessObserveWait(t *testing.T, f freshnessFixture, tx *sql.Tx, pid int, boundary time.Time) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := tx.ExecContext(f.ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
			t.Fatal("clear activity snapshot failed")
		}
		var waiting sql.NullBool
		var started, statement sql.NullTime
		err := tx.QueryRowContext(f.ctx, `SELECT wait_event_type='Lock' AND pg_backend_pid()=ANY(pg_blocking_pids(pid)),xact_start,query_start FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting, &started, &statement)
		if err != nil {
			t.Fatal("observe exact waiter failed")
		}
		if waiting.Valid && waiting.Bool {
			if !started.Valid || !statement.Valid || !started.Time.Before(boundary) || !statement.Time.Before(boundary) {
				t.Fatal("wait began too late to prove clock regression")
			}
			return
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("exact session wait not observed")
		case <-ticker.C:
		}
	}
}
func freshnessCross(t *testing.T, f freshnessFixture, tx *sql.Tx, boundary time.Time) time.Time {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var now time.Time
		if err := tx.QueryRowContext(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal("observe database clock failed")
		}
		if !now.Before(boundary) {
			return now
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("database boundary not crossed")
		case <-ticker.C:
		}
	}
}

func TestSessionFreshnessRequiredServiceWaits(t *testing.T) {
	for _, name := range []string{"idle", "absolute", "live", "live_long", "rollback", "assurance", "revoked", "epoch", "inactive", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newFreshnessFixture(t)
			var before, after, during freshnessTimes
			var boundary, released time.Time
			var got store.AuthenticatedSession
			var lookupErr error
			var level string
			var assuranceExpiry time.Time
			pidCh := make(chan int, 1)
			done := make(chan error, 1)
			joined := make(chan struct{})
			waiterCtx, stop := context.WithCancel(f.ctx)
			defer stop()
			rollback := errors.New("intentional renewal rollback")
			err := f.db.WithTx(f.ctx, nil, func(blocker *sql.Tx) error {
				query := `UPDATE identity_sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING idle_expires_at`
				switch name {
				case "absolute":
					query = `WITH boundary AS MATERIALIZED (SELECT clock_timestamp()+interval '1 second' AS at) UPDATE identity_sessions SET assurance_level='aal1',assurance_expires_at=NULL,idle_expires_at=boundary.at,expires_at=boundary.at FROM boundary WHERE id=$1 RETURNING expires_at`
				case "live_long":
					query = `UPDATE identity_sessions SET expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1 RETURNING clock_timestamp()+interval '1 second'`
				case "assurance":
					query = `UPDATE identity_sessions SET assurance_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING assurance_expires_at`
				case "idle":
				default:
					query = `SELECT clock_timestamp()+interval '1 second' FROM identity_sessions WHERE id=$1 FOR UPDATE`
				}
				if err := blocker.QueryRowContext(f.ctx, query, f.session).Scan(&boundary); err != nil {
					return err
				}
				var err error
				before, err = freshnessRead(f.ctx, blocker, f.session)
				if err != nil {
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
						st, err := store.New(tx)
						if err != nil {
							return err
						}
						got, lookupErr = st.FindActiveSession(waiterCtx, f.digest[:], f.scope)
						if lookupErr != nil {
							if name == "cancel" {
								return lookupErr
							}
							return nil
						}
						during, err = freshnessRead(waiterCtx, tx, f.session)
						if err != nil {
							return err
						}
						level, assuranceExpiry, err = st.ActiveSessionAssurance(waiterCtx, f.session)
						if err != nil {
							return err
						}
						if name == "rollback" {
							return rollback
						}
						return nil
					})
				}()
				// Always cancel and join the worker before fixture cleanup, including failures.
				t.Cleanup(func() {
					stop()
					select {
					case <-joined:
					case <-time.After(5 * time.Second):
						t.Error("waiter cleanup timed out")
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
				freshnessObserveWait(t, f, blocker, pid, boundary)
				switch name {
				case "revoked":
					if _, err := blocker.ExecContext(f.ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session); err != nil {
						return err
					}
				case "epoch":
					if _, err := blocker.ExecContext(f.ctx, `UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, f.person); err != nil {
						return err
					}
				case "inactive":
					if _, err := blocker.ExecContext(f.ctx, `UPDATE identity_persons SET state='self_disabled' WHERE id=$1`, f.person); err != nil {
						return err
					}
				case "cancel":
					stop()
				}
				released = freshnessCross(t, f, blocker, boundary)
				return nil
			})
			if err != nil {
				t.Fatal("blocker transaction failed")
			}
			select {
			case err = <-done:
			case <-f.ctx.Done():
				t.Fatal("waiter did not finish")
			}
			if name == "rollback" {
				if !errors.Is(err, rollback) {
					t.Fatal("rollback not preserved")
				}
			} else if name == "cancel" {
				if !errors.Is(lookupErr, store.ErrPersistence) {
					t.Fatal("canceled lock did not fail persistence")
				}
			} else if err != nil {
				t.Fatal("waiter transaction failed")
			}
			denied := name == "idle" || name == "absolute" || name == "revoked" || name == "epoch" || name == "inactive" || name == "cancel"
			if denied {
				want := store.ErrSessionUnavailable
				if name == "cancel" {
					want = store.ErrPersistence
				}
				if !errors.Is(lookupErr, want) || got != (store.AuthenticatedSession{}) {
					t.Fatalf("stale session admitted: error=%v", lookupErr)
				}
			} else {
				if lookupErr != nil || got.ID != f.session || got.PersonID != f.person {
					t.Fatal("live session rejected")
				}
				expectedIdle := during.absolute
				if name == "live_long" {
					expectedIdle = during.seen.Add(30 * time.Minute)
				}
				if during.seen.Before(released) || !during.idle.Equal(expectedIdle) || !during.absolute.Equal(before.absolute) {
					t.Fatal("renewal not post-wait or not bounded to absolute expiry")
				}
				if name == "assurance" {
					if level != "aal1" || !assuranceExpiry.Equal(before.absolute) {
						t.Fatal("stale elevated assurance retained")
					}
				} else if level != "aal2" {
					t.Fatal("live elevated assurance lost")
				}
			}
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error { var err error; after, err = freshnessRead(f.ctx, tx, f.session); return err }); err != nil {
				t.Fatal("read committed session failed")
			}
			if denied || name == "rollback" {
				if !freshnessSame(before, after) {
					t.Fatal("denied or rolled back renewal changed persisted clocks")
				}
			} else if !freshnessSame(during, after) {
				t.Fatal("committed renewal differs")
			}
		})
	}
}

func TestSessionFreshnessRequiredServiceDenials(t *testing.T) {
	for _, name := range []string{"installation", "application", "environment", "unknown", "repeatable_read", "serializable", "completed", "assurance_idle", "assurance_absolute", "assurance_revoked", "assurance_missing", "assurance_expired"} {
		t.Run(name, func(t *testing.T) {
			f := newFreshnessFixture(t)
			var before, after freshnessTimes
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error { var err error; before, err = freshnessRead(f.ctx, tx, f.session); return err }); err != nil {
				t.Fatal("read original failed")
			}
			options := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
			if name == "repeatable_read" {
				options.Isolation = sql.LevelRepeatableRead
			}
			if name == "serializable" {
				options.Isolation = sql.LevelSerializable
			}
			rollback := errors.New("rollback denial schedule")
			err := f.db.WithTx(f.ctx, options, func(tx *sql.Tx) error {
				st, err := store.New(tx)
				if err != nil {
					return err
				}
				scope, digest := f.scope, f.digest
				want := store.ErrSessionUnavailable
				switch name {
				case "installation":
					scope.InstallationID = newID(t)
				case "application":
					scope.ApplicationID = newID(t)
				case "environment":
					scope.EnvironmentID = newID(t)
				case "unknown":
					digest = sha256.Sum256([]byte("unknown-" + f.session.String()))
				case "repeatable_read", "serializable":
					want = store.ErrPersistence
				case "completed":
					if err := tx.Rollback(); err != nil {
						return err
					}
					want = store.ErrPersistence
				}
				switch name {
				case "assurance_idle", "assurance_absolute", "assurance_revoked", "assurance_missing", "assurance_expired":
					if _, err := st.FindActiveSession(f.ctx, digest[:], scope); err != nil {
						return err
					}
					query := `UPDATE identity_sessions SET idle_expires_at=clock_timestamp() WHERE id=$1`
					id := f.session
					switch name {
					case "assurance_absolute":
						query = `WITH boundary AS MATERIALIZED (SELECT clock_timestamp() AS at) UPDATE identity_sessions SET assurance_level='aal1',assurance_expires_at=NULL,idle_expires_at=boundary.at,expires_at=boundary.at FROM boundary WHERE id=$1`
					case "assurance_revoked":
						query = `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE id=$1`
					case "assurance_missing":
						id = newID(t)
					case "assurance_expired":
						query = `UPDATE identity_sessions SET assurance_expires_at=clock_timestamp() WHERE id=$1`
					}
					if name != "assurance_missing" {
						if _, err := tx.ExecContext(f.ctx, query, id); err != nil {
							return err
						}
					}
					level, expires, err := st.ActiveSessionAssurance(f.ctx, id)
					if name == "assurance_expired" {
						if err != nil || level != "aal1" || !expires.Equal(before.absolute) {
							t.Fatal("expired proof not downgraded")
						}
					} else if !errors.Is(err, store.ErrSessionUnavailable) || level != "" || !expires.IsZero() {
						t.Fatal("assurance denial exposed partial result")
					}
				default:
					got, err := st.FindActiveSession(f.ctx, digest[:], scope)
					if !errors.Is(err, want) || got != (store.AuthenticatedSession{}) {
						t.Fatal("lookup denial exposed partial result or wrong sentinel")
					}
					if name == "completed" {
						level, expires, err := st.ActiveSessionAssurance(f.ctx, f.session)
						if !errors.Is(err, store.ErrPersistence) || level != "" || !expires.IsZero() {
							t.Fatal("completed assurance transaction admitted")
						}
					}
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("denial schedule failed")
			}
			if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error { var err error; after, err = freshnessRead(f.ctx, tx, f.session); return err }); err != nil {
				t.Fatal("read final failed")
			}
			if !freshnessSame(before, after) {
				t.Fatal("denial schedule changed clocks")
			}
		})
	}
}

// This test-only connection forwards every query to a real runtime transaction.
// Immediately after the real database clock sample it sets an owned boundary to
// that exact instant. This deterministic equality probe supplements (and does
// not replace) the unmodified-clock concurrent schedules above. It does not
// simulate a provider or claim that arbitrary post-sample writers serialize.
type freshnessBoundaryConnector struct {
	tx       *sql.Tx
	boundary func(context.Context, time.Time) error
}

func (c *freshnessBoundaryConnector) Connect(context.Context) (driver.Conn, error) {
	return &freshnessBoundaryConn{connector: c}, nil
}
func (c *freshnessBoundaryConnector) Driver() driver.Driver { return freshnessBoundaryDriver{} }

type freshnessBoundaryDriver struct{}

func (freshnessBoundaryDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("boundary probe requires its connector")
}

type freshnessBoundaryConn struct{ connector *freshnessBoundaryConnector }

func (*freshnessBoundaryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("boundary probe only forwards context queries")
}
func (*freshnessBoundaryConn) Close() error              { return nil }
func (*freshnessBoundaryConn) Begin() (driver.Tx, error) { return freshnessBoundaryTx{}, nil }

// The outer RuntimeDB.WithTx owns actual transaction completion. The facade
// only lets the unchanged store method operate on that transaction for this probe.
type freshnessBoundaryTx struct{}

func (freshnessBoundaryTx) Commit() error   { return nil }
func (freshnessBoundaryTx) Rollback() error { return nil }

func (c *freshnessBoundaryConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if query == `SELECT clock_timestamp()` {
		var sampled time.Time
		if err := c.connector.tx.QueryRowContext(ctx, query).Scan(&sampled); err != nil {
			return nil, err
		}
		if err := c.connector.boundary(ctx, sampled); err != nil {
			return nil, err
		}
		return &freshnessSampleRow{sampled: sampled}, nil
	}
	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	rows, err := c.connector.tx.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	return &freshnessForwardRows{Rows: rows, columns: columns}, nil
}

type freshnessSampleRow struct {
	sampled time.Time
	read    bool
}

func (*freshnessSampleRow) Columns() []string { return []string{"clock_timestamp"} }
func (*freshnessSampleRow) Close() error      { return nil }
func (r *freshnessSampleRow) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	values[0] = r.sampled
	return nil
}

type freshnessForwardRows struct {
	*sql.Rows
	columns []string
}

func (r *freshnessForwardRows) Columns() []string { return r.columns }
func (r *freshnessForwardRows) Next(values []driver.Value) error {
	if !r.Rows.Next() {
		if err := r.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	targets := make([]any, len(values))
	for i := range values {
		targets[i] = &values[i]
	}
	return r.Scan(targets...)
}

func TestSessionFreshnessRequiredServiceExactBoundary(t *testing.T) {
	for _, name := range []string{"idle", "absolute", "assurance", "renewal_idle", "renewal_absolute"} {
		t.Run(name, func(t *testing.T) {
			f := newFreshnessFixture(t)
			rollback := errors.New("rollback exact boundary probe")
			err := f.db.WithTx(f.ctx, nil, func(actual *sql.Tx) error {
				st, err := store.New(actual)
				if err != nil {
					return err
				}
				if _, err := st.FindActiveSession(f.ctx, f.digest[:], f.scope); err != nil {
					return err
				}
				var original freshnessTimes
				original, err = freshnessRead(f.ctx, actual, f.session)
				if err != nil {
					return err
				}
				connector := &freshnessBoundaryConnector{tx: actual, boundary: func(ctx context.Context, sampled time.Time) error {
					query := `UPDATE identity_sessions SET assurance_expires_at=$2 WHERE id=$1`
					switch name {
					case "idle", "renewal_idle":
						query = `UPDATE identity_sessions SET idle_expires_at=$2 WHERE id=$1`
					case "absolute", "renewal_absolute":
						query = `UPDATE identity_sessions SET assurance_level='aal1',assurance_expires_at=NULL,idle_expires_at=$2,expires_at=$2 WHERE id=$1`
					}
					_, err := actual.ExecContext(ctx, query, f.session, sampled)
					return err
				}}
				probeDB := sql.OpenDB(connector)
				defer func() {
					if err := probeDB.Close(); err != nil {
						t.Error("boundary facade close failed")
					}
				}()
				probeTx, err := probeDB.BeginTx(f.ctx, nil)
				if err != nil {
					return err
				}
				defer func() {
					if err := probeTx.Rollback(); err != nil {
						t.Error("boundary facade rollback failed")
					}
				}()
				probe, err := store.New(probeTx)
				if err != nil {
					return err
				}
				if name == "renewal_idle" || name == "renewal_absolute" {
					got, err := probe.FindActiveSession(f.ctx, f.digest[:], f.scope)
					if !errors.Is(err, store.ErrSessionUnavailable) || got != (store.AuthenticatedSession{}) {
						t.Fatal("exact session expiry admitted renewal")
					}
					after, err := freshnessRead(f.ctx, actual, f.session)
					if err != nil {
						return err
					}
					if !after.seen.Equal(original.seen) {
						t.Fatal("exact expiry denial updated last seen")
					}
					return rollback
				}
				level, expiry, err := probe.ActiveSessionAssurance(f.ctx, f.session)
				if name == "assurance" {
					if err != nil || level != "aal1" || !expiry.Equal(original.absolute) {
						t.Fatal("exact assurance expiry was not downgraded")
					}
				} else if !errors.Is(err, store.ErrSessionUnavailable) || level != "" || !expiry.IsZero() {
					t.Fatal("exact session expiry admitted assurance")
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("real database exact-boundary probe failed")
			}
		})
	}
}
