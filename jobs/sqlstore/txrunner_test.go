package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var _ func(*sql.DB, Config) (*Store, error) = New
var _ func(TxRunner, Config) (*Store, error) = NewWithTx
var _ TxRunner = (*storage.RuntimeDB)(nil)

type runnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f runnerFunc) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type nilMapRunner map[string]string

func (nilMapRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected constructor I/O")
}

type nilSliceRunner []string

func (nilSliceRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("unexpected constructor I/O")
}
func unitConfig() Config {
	return Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 4, MaxLease: 5 * time.Second, MaxRetryDelay: time.Second}
}

func TestTxRunnerConstruction(t *testing.T) {
	cfg := unitConfig()
	for _, r := range []TxRunner{nil, (*storage.RuntimeDB)(nil), runnerFunc(nil), nilMapRunner(nil), nilSliceRunner(nil)} {
		if s, err := NewWithTx(r, cfg); s != nil || !errors.Is(err, ErrInvalidConfig) {
			t.Fatal("nil runner accepted")
		}
	}
	calls := 0
	r := runnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { calls++; return nil })
	cfg.ClaimKinds = []string{"billing.webhook.reconcile"}
	cfg.ClaimScopes = []ClaimScope{newClaimScope(t)}
	s, err := NewWithTx(r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClaimKinds[0] = "changed"
	cfg.ClaimScopes[0].Provider = "changed"
	if s.config.ClaimKinds[0] == "changed" || s.config.ClaimScopes[0].Provider == "changed" || calls != 0 {
		t.Fatal("construction did I/O or retained config")
	}
	if _, err := NewWithTx(r, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if _, err := New(nil, cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if _, err := NewTxWriter(unitConfig()); err != nil {
		t.Fatal(err)
	}
}

// The deterministic driver qualifies transaction mechanics only, never SQL.
type txProbe struct {
	begins, commits, rollbacks, queries, execs int
	options                                    driver.TxOptions
	commitErr, rollbackErr                     error
	empty                                      bool
}
type probeConnector struct{ p *txProbe }

func (c probeConnector) Connect(context.Context) (driver.Conn, error) { return &probeConn{p: c.p}, nil }
func (c probeConnector) Driver() driver.Driver                        { return probeDriver(c) }

type probeDriver struct{ p *txProbe }

func (d probeDriver) Open(string) (driver.Conn, error) { return &probeConn{p: d.p}, nil }

type probeConn struct{ p *txProbe }

func (c *probeConn) Prepare(string) (driver.Stmt, error)      { return nil, errors.New("unsupported") }
func (c *probeConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (c *probeConn) Close() error                             { return nil }
func (c *probeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *probeConn) BeginTx(_ context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.p.begins++
	c.p.options = o
	return probeTx{p: c.p}, nil
}

type probeTx struct{ p *txProbe }

func (x probeTx) Commit() error   { x.p.commits++; return x.p.commitErr }
func (x probeTx) Rollback() error { x.p.rollbacks++; return x.p.rollbackErr }
func (c *probeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.p.execs++
	return driver.RowsAffected(1), nil
}
func (c *probeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.p.queries++
	id := "01900000-0000-7000-8000-000000000001"
	if strings.Contains(q, "SELECT clock_timestamp()") {
		return &probeRows{values: []driver.Value{time.Now()}}, nil
	}
	if strings.HasPrefix(q, "INSERT") || strings.Contains(q, "SELECT id FROM") {
		return &probeRows{values: []driver.Value{id}}, nil
	}
	if c.p.empty && strings.Contains(q, "WITH candidate") {
		return &probeRows{done: true, values: make([]driver.Value, 20)}, nil
	}
	return &probeRows{values: []driver.Value{id, id, id, "key", make([]byte, 32), "email.send", "{}", true, "leased", int64(1), int64(3), int64(0), int64(4), time.Now().Add(time.Hour), "worker", "execute", int64(1), time.Now().Add(time.Second), false, time.Now()}}, nil
}

type probeRows struct {
	values []driver.Value
	done   bool
}

func (r *probeRows) Columns() []string { return make([]string, len(r.values)) }
func (r *probeRows) Close() error      { return nil }
func (r *probeRows) Next(v []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(v, r.values)
	r.done = true
	return nil
}
func probeDB(t *testing.T, p *txProbe) *sql.DB {
	t.Helper()
	db := sql.OpenDB(probeConnector{p})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestTxRunnerOperations(t *testing.T) {
	for _, name := range []string{"enqueue", "get", "claim", "resolve", "empty"} {
		for _, fail := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/success", true: "/commit-failure"}[fail], func(t *testing.T) {
				p := &txProbe{empty: name == "empty"}
				if fail {
					p.commitErr = errors.New("private driver detail")
				}
				db := probeDB(t, p)
				s, err := NewWithTx(poolRunner{db}, unitConfig())
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				var got jobs.Job
				var ok bool
				switch name {
				case "enqueue":
					got, err = s.Enqueue(ctx, intent(t, "unit", true))
				case "get":
					got, err = s.Get(ctx, uuid.New())
				case "claim", "empty":
					got, ok, err = s.Claim(ctx, "worker", time.Second)
				case "resolve":
					err = s.Resolve(ctx, jobs.Job{ID: uuid.New(), FenceToken: 1, Action: jobs.ActionExecute}, "worker", jobs.Resolution{Kind: jobs.ResolutionSucceeded})
				}
				if fail {
					if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(got, jobs.Job{}) || ok {
						t.Fatalf("transaction failure leaked result: %v %v", ok, err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if p.begins != 1 || p.commits != 1 || p.options.Isolation != driver.IsolationLevel(sql.LevelReadCommitted) || p.options.ReadOnly != (name == "get") {
					t.Fatalf("transaction/options mismatch: %+v", p)
				}
				if name == "empty" && (ok || p.execs != 2) {
					t.Fatal("empty claim did not finish maintenance")
				}
			})
		}
	}
}
func TestTxRunnerErrorsAndLegacyOwnership(t *testing.T) {
	p := &txProbe{}
	db := probeDB(t, p)
	s, err := New(db, unitConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if p.options != (driver.TxOptions{}) {
		t.Fatal("legacy options changed")
	}
	if err = db.Ping(); err != nil {
		t.Fatal("store closed caller pool")
	}
	for _, rollbackFailure := range []bool{false, true} {
		p.rollbackErr = nil
		if rollbackFailure {
			p.rollbackErr = errors.New("private rollback detail")
		}
		_, err = s.Enqueue(context.Background(), jobs.Intent{})
		if !errors.Is(err, jobs.ErrInvalidIntent) || errors.Is(err, ErrUnavailable) != rollbackFailure || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe rollback mapping", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Get(ctx, uuid.New())
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrUnavailable) {
		t.Fatal("lost cancellation", err)
	}
	marker := &struct{}{}
	func() {
		defer func() {
			if recover() != marker {
				t.Error("panic identity lost")
			}
		}()
		_ = (poolRunner{db}).WithTx(context.Background(), nil, func(*sql.Tx) error { panic(marker) })
	}()
	if p.rollbacks != 3 {
		t.Fatal("rollback omitted", p.rollbacks)
	}
}

// This suite requires an operator-precreated TLS database and DML-only runtime
// configuration. It never creates schema or reads administrative credentials.
func TestJobStoreRuntimeRequiredService(t *testing.T) {
	path := os.Getenv("AMOS_JOBS_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required jobs TLS PostgreSQL fixture absent: set AMOS_JOBS_RUNTIME_TEST_CONFIG")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("required jobs runtime configuration cannot be read")
	}
	var input struct {
		Host     string `json:"host"`
		Port     uint16 `json:"port"`
		Database string `json:"database"`
		User     string `json:"user"`
		Password string `json:"password"`
		CAPath   string `json:"ca_path"`
	}
	if json.Unmarshal(data, &input) != nil {
		t.Fatal("required jobs runtime configuration invalid")
	}
	roots, err := os.ReadFile(input.CAPath)
	if err != nil {
		t.Fatal("required jobs runtime CA cannot be read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: input.Host, Port: input.Port, Database: input.Database, User: input.User, Password: input.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required jobs runtime connection failed")
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error("runtime close failed")
		}
	}()
	scope := newClaimScope(t)
	cfg := unitConfig()
	cfg.ClaimKinds = []string{"billing.webhook.reconcile"}
	cfg.ClaimScopes = []ClaimScope{scope}
	s, err := NewWithTx(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var owned []uuid.UUID
	// All fixture mutation is serial and cleanup addresses exact recorded UUIDs.
	defer func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := db.WithTx(cleanCtx, nil, func(tx *sql.Tx) error {
			for _, id := range owned {
				if _, err := tx.ExecContext(cleanCtx, `DELETE FROM public.amos_jobs WHERE id=$1`, id); err != nil {
					return ErrUnavailable
				}
				if _, err := tx.ExecContext(cleanCtx, `DELETE FROM public.fixture_domain_rows WHERE id=$1`, id); err != nil {
					return ErrUnavailable
				}
			}
			return nil
		}); err != nil {
			t.Error("exact-owned fixture cleanup failed")
		}
	}()
	enqueue := func(key string, external bool) jobs.Job {
		t.Helper()
		in := claimScopeIntent(t, key, scope)
		in.ExternalEffect = external
		j, err := s.Enqueue(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		owned = append(owned, j.ID)
		return j
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q, args...); return err }); err != nil {
			t.Fatal("fixture DML failed")
		}
	}
	claim := func() jobs.Job {
		t.Helper()
		j, ok, err := s.Claim(ctx, "runtime-worker", 4*time.Second)
		if err != nil || !ok {
			t.Fatal("runtime claim failed", err)
		}
		return j
	}
	resolve := func(j jobs.Job, kind jobs.ResolutionKind) {
		t.Helper()
		if err := s.Resolve(ctx, j, "runtime-worker", jobs.Resolution{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("idempotency-concurrency", func(t *testing.T) {
		in := claimScopeIntent(t, "concurrent", scope)
		var wg sync.WaitGroup
		results := make(chan jobs.Job, 2)
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				j, e := s.Enqueue(ctx, in)
				if e != nil {
					errs <- e
				} else {
					results <- j
				}
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			t.Fatal(e)
		}
		var id uuid.UUID
		for j := range results {
			if id != uuid.Nil && id != j.ID {
				t.Fatal("idempotency duplicated")
			}
			id = j.ID
		}
		if id == uuid.Nil {
			t.Fatal("no result")
		}
		owned = append(owned, id)
		in.Payload = []byte(`{"changed":true}`)
		if _, e := s.Enqueue(ctx, in); !errors.Is(e, jobs.ErrIdempotencyConflict) {
			t.Fatal("conflict lost", e)
		}
		j := claim()
		if j.ID != id {
			t.Fatal("wrong claim")
		}
		resolve(j, jobs.ResolutionSucceeded)
	})
	t.Run("concurrent-claim", func(t *testing.T) {
		created := enqueue("two-workers", false)
		var wg sync.WaitGroup
		claimed := make(chan jobs.Job, 2)
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				j, ok, e := s.Claim(ctx, "runtime-worker", 4*time.Second)
				if e != nil {
					errs <- e
				} else if ok {
					claimed <- j
				}
			}()
		}
		wg.Wait()
		close(claimed)
		close(errs)
		for e := range errs {
			t.Fatal(e)
		}
		count := 0
		for j := range claimed {
			count++
			if j.ID != created.ID {
				t.Fatal("wrong concurrent claim")
			}
			resolve(j, jobs.ResolutionSucceeded)
		}
		if count != 1 {
			t.Fatal("concurrent claims", count)
		}
	})

	t.Run("joint-rollback-and-commit", func(t *testing.T) {
		writer, e := NewTxWriter(cfg)
		if e != nil {
			t.Fatal(e)
		}
		for _, commit := range []bool{false, true} {
			domain := newClaimUUID(t)
			owned = append(owned, domain)
			var job jobs.Job
			abort := errors.New("domain abort")
			e = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				if _, e := tx.ExecContext(ctx, `INSERT INTO public.fixture_domain_rows(id,value) VALUES($1,'joint')`, domain); e != nil {
					return ErrUnavailable
				}
				var e error
				job, e = writer.EnqueueTx(ctx, tx, claimScopeIntent(t, domain.String(), scope))
				if e != nil {
					return e
				}
				if !commit {
					return abort
				}
				return nil
			})
			owned = append(owned, job.ID)
			if commit && e != nil || !commit && !errors.Is(e, abort) {
				t.Fatal("joint transaction failed", e)
			}
			got, e := s.Get(ctx, job.ID)
			if commit {
				if e != nil || got.ID != job.ID {
					t.Fatal("committed intent absent", e)
				}
				resolve(claim(), jobs.ResolutionSucceeded)
			} else if !errors.Is(e, jobs.ErrNotFound) {
				t.Fatal("rolled intent visible", e)
			}
			var count int
			if e = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				return tx.QueryRowContext(ctx, `SELECT count(*) FROM public.fixture_domain_rows WHERE id=$1`, domain).Scan(&count)
			}); e != nil || count != map[bool]int{false: 0, true: 1}[commit] {
				t.Fatal("domain atomicity failed")
			}
		}
	})
	t.Run("scope-maintenance-and-empty-commit", func(t *testing.T) {
		foreign := scope
		foreign.EnvironmentID = newClaimUUID(t)
		var foreignIDs []uuid.UUID
		for _, key := range []string{"foreign-queued", "foreign-deadline", "foreign-lease"} {
			j, e := s.Enqueue(ctx, claimScopeIntent(t, key, foreign))
			if e != nil {
				t.Fatal(e)
			}
			owned = append(owned, j.ID)
			foreignIDs = append(foreignIDs, j.ID)
		}
		exec(`UPDATE public.amos_jobs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id=$1`, foreignIDs[1])
		exec(`UPDATE public.amos_jobs SET status='leased',lease_owner='foreign',lease_action='execute',lease_until=clock_timestamp()-interval '1 second',attempt_count=1 WHERE id=$1`, foreignIDs[2])
		local := enqueue("expired-local", false)
		exec(`UPDATE public.amos_jobs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id=$1`, local.ID)
		if _, ok, e := s.Claim(ctx, "runtime-worker", time.Second); e != nil || ok {
			t.Fatal("foreign claim or empty error", e)
		}
		got, e := s.Get(ctx, local.ID)
		if e != nil || got.State != jobs.StateDead {
			t.Fatal("empty maintenance not committed")
		}
		for i, id := range foreignIDs {
			got, e := s.Get(ctx, id)
			want := jobs.StateQueued
			if i == 2 {
				want = jobs.StateLeased
			}
			if e != nil || got.State != want || i == 2 && got.LeaseOwner != "foreign" {
				t.Fatal("foreign scope maintained")
			}
		}
	})
	t.Run("unknown-reconciliation-budget", func(t *testing.T) {
		enqueue("unknown", true)
		j := claim()
		resolve(j, jobs.ResolutionUnknown)
		for attempt := 1; attempt <= cfg.MaxReconciliationAttempts; attempt++ {
			j = claim()
			if j.Action != jobs.ActionReconcile || j.Attempt != 1 || j.ReconciliationAttempt != attempt {
				t.Fatal("unknown replayed or budget mixed")
			}
			resolve(j, jobs.ResolutionUnknown)
		}
		got, e := s.Get(ctx, j.ID)
		if e != nil || !got.ManualReview || got.State != jobs.StateUnknown {
			t.Fatal("unknown budget lost")
		}
	})
	t.Run("post-lock-expiry", func(t *testing.T) {
		enqueue("lock-expiry", false)
		j := claim()
		locked := make(chan int, 1)
		release := make(chan struct{})
		blockDone := make(chan error, 1)
		go func() {
			blockDone <- db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var id uuid.UUID
				if e := tx.QueryRowContext(ctx, `SELECT id FROM public.amos_jobs WHERE id=$1 FOR UPDATE`, j.ID).Scan(&id); e != nil {
					return e
				}
				var pid int
				if e := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
					return e
				}
				locked <- pid
				// Observe an actual waiter while retaining the unchanged row lock, then
				// observe database time pass the lease before releasing it.
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				waiting := false
				for {
					var now time.Time
					var blocked bool
					if e := tx.QueryRowContext(ctx, `SELECT clock_timestamp(), EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&now, &blocked); e != nil {
						return e
					}
					waiting = waiting || blocked
					if waiting && now.After(j.LeaseUntil) {
						return nil
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-release:
						return errors.New("test aborted")
					case <-ticker.C:
					}
				}
			})
		}()
		select {
		case <-locked:
		case <-ctx.Done():
			t.Fatal("blocker not ready")
		}
		e := s.Resolve(ctx, j, "runtime-worker", jobs.Resolution{Kind: jobs.ResolutionSucceeded})
		close(release)
		if blockerErr := <-blockDone; blockerErr != nil {
			t.Fatal("lock observation failed")
		}
		if !errors.Is(e, jobs.ErrLeaseLost) {
			t.Fatal("expired lock waiter accepted", e)
		}
		got, e := s.Get(ctx, j.ID)
		if e != nil || got.State != jobs.StateLeased {
			t.Fatal("stale resolution changed row")
		}
		exec(`UPDATE public.amos_jobs SET status='dead',lease_owner=NULL,lease_action=NULL,lease_until=NULL WHERE id=$1`, j.ID)
	})
	t.Run("pool-admission-cancellation", func(t *testing.T) {
		ready := make(chan struct{}, 2)
		release := make(chan struct{})
		done := make(chan error, 2)
		for range 2 {
			go func() {
				done <- db.WithTx(ctx, nil, func(*sql.Tx) error {
					ready <- struct{}{}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
		}
		for range 2 {
			select {
			case <-ready:
			case <-ctx.Done():
				close(release)
				t.Fatal("pool holders absent")
			}
		}
		wait, cancelWait := context.WithTimeout(ctx, 50*time.Millisecond)
		j, e := s.Get(wait, newClaimUUID(t))
		cancelWait()
		close(release)
		for range 2 {
			if e := <-done; e != nil {
				t.Fatal("holder failed")
			}
		}
		if !errors.Is(e, context.DeadlineExceeded) || !errors.Is(e, ErrUnavailable) || !reflect.DeepEqual(j, jobs.Job{}) {
			t.Fatal("pool admission cancellation lost", e)
		}
		if e := db.PingContext(ctx); e != nil {
			t.Fatal("pool capacity not recovered")
		}
	})
	t.Run("lock-wait-cancellation", func(t *testing.T) {
		enqueue("cancel-lock", false)
		j := claim()
		ready := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var id uuid.UUID
				if e := tx.QueryRowContext(ctx, `SELECT id FROM public.amos_jobs WHERE id=$1 FOR UPDATE`, j.ID).Scan(&id); e != nil {
					return e
				}
				close(ready)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		select {
		case <-ready:
		case <-ctx.Done():
			close(release)
			t.Fatal("lock holder absent")
		}
		wait, cancelWait := context.WithTimeout(ctx, 100*time.Millisecond)
		e := s.Resolve(wait, j, "runtime-worker", jobs.Resolution{Kind: jobs.ResolutionSucceeded})
		cancelWait()
		close(release)
		if e := <-done; e != nil {
			t.Fatal("lock holder failed")
		}
		if !errors.Is(e, context.DeadlineExceeded) || !errors.Is(e, ErrUnavailable) {
			t.Fatal("lock cancellation lost", e)
		}
		resolve(j, jobs.ResolutionSucceeded)
	})

	t.Run("cancellation-and-commit-suppression", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		j, e := s.Get(canceled, newClaimUUID(t))
		if !errors.Is(e, context.Canceled) || !errors.Is(e, ErrUnavailable) || !reflect.DeepEqual(j, jobs.Job{}) {
			t.Fatal("cancellation mapping lost", e)
		}
		// End the real transaction after callback success so RuntimeDB commit
		// actually fails. This does not simulate an uncertain server commit.
		failing := runnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
			return db.WithTx(c, o, func(tx *sql.Tx) error {
				if e := fn(tx); e != nil {
					return e
				}
				return tx.Rollback()
			})
		})
		failed, e := NewWithTx(failing, cfg)
		if e != nil {
			t.Fatal(e)
		}
		j, e = failed.Enqueue(ctx, claimScopeIntent(t, "abort-after-callback", scope))
		if !errors.Is(e, ErrUnavailable) || !reflect.DeepEqual(j, jobs.Job{}) {
			t.Fatal("provisional job escaped")
		}
		if _, ok, e := s.Claim(ctx, "runtime-worker", time.Second); e != nil || ok {
			t.Fatal("aborted intent persisted", e)
		}
		created := enqueue("commit-failure-results", false)
		j, e = failed.Get(ctx, created.ID)
		if !errors.Is(e, ErrUnavailable) || !reflect.DeepEqual(j, jobs.Job{}) {
			t.Fatal("failed Get exposed result")
		}
		j, ok, e := failed.Claim(ctx, "runtime-worker", time.Second)
		if !errors.Is(e, ErrUnavailable) || ok || !reflect.DeepEqual(j, jobs.Job{}) {
			t.Fatal("failed Claim exposed result")
		}
		current := claim()
		if current.ID != created.ID || current.Attempt != 1 {
			t.Fatal("failed Claim changed row")
		}
		e = failed.Resolve(ctx, current, "runtime-worker", jobs.Resolution{Kind: jobs.ResolutionSucceeded})
		if !errors.Is(e, ErrUnavailable) {
			t.Fatal("failed Resolve acknowledged")
		}
		stored, e := s.Get(ctx, current.ID)
		if e != nil || stored.State != jobs.StateLeased {
			t.Fatal("failed Resolve persisted")
		}
		resolve(current, jobs.ResolutionSucceeded)

	})
}

func TestTxRunnerCallerOwnedWriter(t *testing.T) {
	p := &txProbe{}
	db := probeDB(t, p)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewTxWriter(unitConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.EnqueueTx(context.Background(), tx, intent(t, "caller-owned", false)); err != nil {
		t.Fatal(err)
	}
	if p.begins != 1 || p.commits != 0 || p.rollbacks != 0 {
		t.Fatal("writer owned transaction")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.EnqueueTx(canceled, tx, intent(t, "canceled-writer", false)); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrUnavailable) {
		t.Fatal("caller transaction cancellation lost", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestTxRunnerBeginFailureAndInvalidCalls(t *testing.T) {
	calls := 0
	r := runnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
		calls++
		return errors.New("private connection detail")
	})
	s, err := NewWithTx(r, unitConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, name := range []string{"enqueue", "get", "claim", "resolve"} {
		var j jobs.Job
		var ok bool
		switch name {
		case "enqueue":
			j, err = s.Enqueue(ctx, intent(t, "begin", false))
		case "get":
			j, err = s.Get(ctx, uuid.New())
		case "claim":
			j, ok, err = s.Claim(ctx, "owner", time.Second)
		case "resolve":
			err = s.Resolve(ctx, jobs.Job{ID: uuid.New(), FenceToken: 1}, "owner", jobs.Resolution{Kind: jobs.ResolutionSucceeded})
		}
		if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private") || !reflect.DeepEqual(j, jobs.Job{}) || ok {
			t.Fatal("begin failure escaped", name, err)
		}
	}
	if calls != 4 {
		t.Fatal("runner calls", calls)
	}
	var nilCtx context.Context
	if _, err := s.Get(nilCtx, uuid.New()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(nilCtx, jobs.Intent{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if _, _, err := s.Claim(ctx, "", time.Second); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if err := s.Resolve(ctx, jobs.Job{}, "", jobs.Resolution{}); !errors.Is(err, jobs.ErrInvalidResolution) {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("invalid input invoked runner")
	}
}
