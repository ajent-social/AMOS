package operation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func callbackUnitDB(t *testing.T) (*invocationConn, DBTX, func() error) {
	t.Helper()
	conn := &invocationConn{result: driver.RowsAffected(1)}
	pool := sql.OpenDB(invocationDriver{conn: conn})
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	tx, err := pool.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	})
	db, invalidate, err := newCallbackDB(tx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = invalidate() })
	return conn, db, invalidate
}

func TestCallbackDBShape(t *testing.T) {
	if db, stop, err := newCallbackDB(nil); db != nil || stop != nil || !errors.Is(err, errNilSQLTx) {
		t.Fatal("nil factory")
	}
	_, db, stop := callbackUnitDB(t)
	typ := reflect.TypeOf(db)
	if typ.NumMethod() != 3 {
		t.Fatal("callback method leak")
	}
	for i := 0; i < typ.Elem().NumField(); i++ {
		field := typ.Elem().Field(i)
		if field.IsExported() || field.Anonymous {
			t.Fatal("callback field leak")
		}
	}
	row := db.QueryRowContext(context.Background(), "SELECT 1")
	if _, ok := row.(*sql.Row); ok {
		t.Fatal("raw Row leaked")
	}
	if err := row.Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(new(int)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("second Scan", err)
	}
	var rawBytes sql.RawBytes
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&rawBytes); err == nil {
		t.Fatal("Row accepted RawBytes")
	}
	if reflect.TypeOf(row).NumMethod() != 1 {
		t.Fatal("Row lifecycle leak")
	}
	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rows.(*sql.Rows); ok {
		t.Fatal("raw Rows leaked")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackDBErrors(t *testing.T) {
	conn, db, stop := callbackUnitDB(t)
	original := errors.New("driver error")
	conn.err = original
	if result, err := db.ExecContext(context.Background(), "SELECT 1"); result != nil || err != original {
		t.Fatal("exec error", err)
	}
	if rows, err := db.QueryContext(context.Background(), "SELECT 1"); rows != nil || err != original {
		t.Fatal("query error", err)
	}
	row := db.QueryRowContext(context.Background(), "SELECT 1")
	if err := row.Scan(new(int)); err != original {
		t.Fatal("row error", err)
	}
	conn.err = nil
	var nilContext context.Context
	if _, err := db.ExecContext(nilContext, "SELECT 1"); err != errCallbackContext {
		t.Fatal(err)
	}
	if rows, err := db.QueryContext(nilContext, "SELECT 1"); rows != nil || err != errCallbackContext {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(nilContext, "SELECT 1").Scan(new(int)); err != errCallbackContext {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(new(int)); err != errCallbackClosed {
		t.Fatal("closed must replace old error", err)
	}
}

func TestCallbackDBRetained(t *testing.T) {
	for _, kind := range []string{"row", "rows", "exhausted", "closed", "consumed"} {
		t.Run(kind, func(t *testing.T) {
			conn, db, stop := callbackUnitDB(t)
			var row Row
			var rows Rows
			var err error
			if kind == "row" || kind == "consumed" {
				row = db.QueryRowContext(context.Background(), "SELECT 1")
				if kind == "consumed" {
					if err := row.Scan(new(int)); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				rows, err = db.QueryContext(context.Background(), "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				if kind == "exhausted" {
					for rows.Next() {
					}
				}
				if kind == "closed" {
					if err := rows.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = stop()
			if (kind == "row" || kind == "rows") != errors.Is(err, errCallbackUnfinished) {
				t.Fatal("unfinished classification", err)
			}
			if stop() != err {
				t.Fatal("invalidation result changed")
			}
			conn.query = ""
			dest := 991
			if row != nil {
				if err := row.Scan(&dest); err != errCallbackClosed {
					t.Fatal("retained Row", err)
				}
			}
			if rows != nil {
				if rows.Next() || rows.Err() != errCallbackClosed || rows.Scan(&dest) != errCallbackClosed || rows.Close() != errCallbackClosed {
					t.Fatal("retained Rows")
				}
			}
			if dest != 991 {
				t.Fatal("post-close destination mutated")
			}
			var nilContext context.Context
			if result, err := db.ExecContext(nilContext, "COMMIT"); result != nil || err != errCallbackClosed {
				t.Fatal(err)
			}
			if result, err := db.QueryContext(nilContext, "COMMIT"); result != nil || err != errCallbackClosed {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(nilContext, "COMMIT").Scan(&dest); err != errCallbackClosed {
				t.Fatal(err)
			}
			if conn.query != "" {
				t.Fatal("post-close driver call")
			}
		})
	}
}

type callbackScanFunc func(any) error

func (f callbackScanFunc) Scan(src any) error { return f(src) }

func TestCallbackDBLeaseAndScannerPanic(t *testing.T) {
	for _, kind := range []string{"row", "rows"} {
		t.Run(kind, func(t *testing.T) {
			_, db, stop := callbackUnitDB(t)
			var scan func(...any) error
			if kind == "row" {
				scan = db.QueryRowContext(context.Background(), "SELECT 1").Scan
			} else {
				rows, err := db.QueryContext(context.Background(), "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				if !rows.Next() {
					t.Fatal("no row")
				}
				scan = rows.Scan
			}
			if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != errCallbackBusy {
				t.Fatal("lease lost", err)
			}
			if rows, err := db.QueryContext(context.Background(), "SELECT 1"); rows != nil || err != errCallbackBusy {
				t.Fatal("query not busy", err)
			}
			if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(new(int)); err != errCallbackBusy {
				t.Fatal("row not busy", err)
			}
			value := &struct{ token int }{73}
			var caught any
			func() {
				defer func() { caught = recover() }()
				_ = scan(callbackScanFunc(func(any) error {
					if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != errCallbackBusy {
						t.Error("reentrant call not busy", err)
					}
					panic(value)
				}))
			}()
			if caught != value {
				t.Fatal("panic identity changed")
			}
			if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != nil {
				t.Fatal("panic leaked lease", err)
			}
			if err := stop(); err != nil {
				t.Fatal("panic drain failed", err)
			}
		})
	}
}

// These controlled results exercise guard synchronization, not provider semantics.
type callbackControlledDB struct {
	exec  func(context.Context) (sql.Result, error)
	query func(context.Context) (Rows, error)
}

func (d callbackControlledDB) ExecContext(c context.Context, _ string, _ ...any) (sql.Result, error) {
	return d.exec(c)
}
func (d callbackControlledDB) QueryContext(c context.Context, _ string, _ ...any) (Rows, error) {
	return d.query(c)
}
func (callbackControlledDB) QueryRowContext(context.Context, string, ...any) Row {
	panic("guard must use QueryContext")
}

type callbackControlledRows struct {
	next     func() bool
	scan     func(...any) error
	err      error
	closeErr error
	closes   int
}

func (r *callbackControlledRows) Next() bool {
	if r.next != nil {
		return r.next()
	}
	return true
}
func (r *callbackControlledRows) Scan(d ...any) error {
	if r.scan != nil {
		return r.scan(d...)
	}
	return nil
}
func (r *callbackControlledRows) Err() error   { return r.err }
func (r *callbackControlledRows) Close() error { r.closes++; return r.closeErr }

func callbackControlled(d DBTX) (*callbackDB, func() error) {
	s := &callbackState{db: d, drained: make(chan struct{})}
	return &callbackDB{state: s}, s.invalidate
}

func callbackWait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
		t.Fatal("barrier deadline")
	}
}

func TestCallbackDBDrain(t *testing.T) {
	for _, method := range []string{"exec", "query", "next", "scan"} {
		t.Run(method, func(t *testing.T) {
			entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			block := func(ctx context.Context) {
				close(entered)
				<-ctx.Done()
				once.Do(func() { close(cancelled) })
				<-release
			}
			var requestCtx context.Context
			raw := &callbackControlledRows{}
			d := callbackControlledDB{
				exec: func(ctx context.Context) (sql.Result, error) { block(ctx); return nil, ctx.Err() },
				query: func(ctx context.Context) (Rows, error) {
					requestCtx = ctx
					if method == "query" {
						block(ctx)
					}
					return raw, nil
				},
			}
			db, stop := callbackControlled(d)
			var rows Rows
			if method == "next" || method == "scan" {
				var err error
				rows, err = db.QueryContext(context.Background(), "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				raw.next = func() bool { block(requestCtx); return false }
				raw.scan = func(...any) error { block(requestCtx); return nil }
			}
			methodDone := make(chan struct{})
			go func() {
				defer close(methodDone)
				switch method {
				case "exec":
					_, _ = db.ExecContext(context.Background(), "SELECT 1")
				case "query":
					_, _ = db.QueryContext(context.Background(), "SELECT 1")
				case "next":
					rows.Next()
				case "scan":
					_ = rows.Scan(new(int))
				}
			}()
			callbackWait(t, entered)
			if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != errCallbackBusy {
				t.Fatal(err)
			}
			if rows != nil {
				if rows.Next() || rows.Scan(new(int)) != errCallbackBusy || rows.Close() != errCallbackBusy || rows.Err() != errCallbackBusy {
					t.Fatal("result admission not busy")
				}
			}
			stopped := make(chan struct{})
			var stopErr error
			go func() { stopErr = stop(); close(stopped) }()
			callbackWait(t, cancelled)
			select {
			case <-stopped:
				t.Fatal("invalidation did not drain")
			default:
			}
			close(release)
			callbackWait(t, methodDone)
			callbackWait(t, stopped)
			if !errors.Is(stopErr, errCallbackUnfinished) {
				t.Fatal(stopErr)
			}
			if method != "exec" && raw.closes != 1 {
				t.Fatal("close count", raw.closes)
			}
			if stop() != stopErr {
				t.Fatal("changed cleanup error")
			}
		})
	}
}

func TestCallbackDBCloseErrorAndNoRows(t *testing.T) {
	original := errors.New("private driver diagnostic")
	raw := &callbackControlledRows{next: func() bool { return false }, closeErr: original}
	db, stop := callbackControlled(callbackControlledDB{query: func(context.Context) (Rows, error) { return raw, nil }})
	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() || rows.Err() != original {
		t.Fatal("terminal error lost")
	}
	if err := rows.Close(); err != nil {
		t.Fatal("close not idempotent")
	}
	if err := stop(); !errors.Is(err, errCallbackCleanup) || errors.Is(err, original) {
		t.Fatal("cleanup diagnostic leak", err)
	}
	if raw.closes != 1 {
		t.Fatal("double close")
	}
	raw = &callbackControlledRows{next: func() bool { return false }}
	db, stop = callbackControlled(callbackControlledDB{query: func(context.Context) (Rows, error) { return raw, nil }})
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(new(int)); err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackDBScannerDrain(t *testing.T) {
	conn, db, stop := callbackUnitDB(t)
	row := db.QueryRowContext(context.Background(), "SELECT 1")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = row.Scan(callbackScanFunc(func(any) error { close(entered); <-release; return nil }))
	}()
	callbackWait(t, entered)
	stopped := make(chan struct{})
	var stopErr error
	go func() { stopErr = stop(); close(stopped) }()
	callbackWait(t, conn.ctx.Done())

	select {
	case <-stopped:
		t.Fatal("Scanner not drained")
	default:
	}
	close(release)
	callbackWait(t, done)
	callbackWait(t, stopped)
	if !errors.Is(stopErr, errCallbackUnfinished) {
		t.Fatal(stopErr)
	}
}

func TestCallbackDBRequiredService(t *testing.T) {
	pool, table := invocationServicePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	var owned []int64
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, id := range owned {
			if _, err := pool.ExecContext(cleanup, "DELETE FROM "+table+" WHERE id=$1", id); err != nil {
				t.Error("exact-owned row cleanup failed")
			}
		}
	})
	begin := func(t *testing.T) (*sql.Tx, DBTX, func() error) {
		t.Helper()
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("begin failed")
		}
		db, stop, err := newCallbackDB(tx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = stop()
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				t.Error("rollback failed")
			}
		})
		return tx, db, stop
	}
	insert := func(t *testing.T, db DBTX) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, "INSERT INTO "+table+" (value) VALUES ($1) RETURNING id", "callback-test").Scan(&id); err != nil {
			t.Fatal("insert failed")
		}
		owned = append(owned, id)
		return id
	}
	t.Run("transaction ownership and controls", func(t *testing.T) {
		tx, db, stop := begin(t)
		id := insert(t, db)
		var ownerID, guardID string
		if tx.QueryRowContext(ctx, "SELECT pg_current_xact_id()::text").Scan(&ownerID) != nil || db.QueryRowContext(ctx, "SELECT pg_current_xact_id()::text").Scan(&guardID) != nil || ownerID != guardID {
			t.Fatal("transaction identity changed")
		}
		var count int
		if pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != 0 {
			t.Fatal("uncommitted write visible")
		}
		for _, q := range callbackControlSQL {
			for _, text := range []string{q, "/*lead*/ " + q, "--lead\n" + q} {
				if result, err := db.ExecContext(ctx, text); result != nil || err != errCallbackSQL {
					t.Fatal("control Exec admitted")
				}
				if rows, err := db.QueryContext(ctx, text); rows != nil || err != errCallbackSQL {
					t.Fatal("control Query admitted")
				}
				if err := db.QueryRowContext(ctx, text).Scan(&count); err != errCallbackSQL {
					t.Fatal("control Row admitted")
				}
			}
		}
		if db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != 1 {
			t.Fatal("control rejection changed transaction")
		}
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal("owner rollback failed")
		}
		if pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != 0 {
			t.Fatal("rollback failed to undo writes")
		}
	})
	t.Run("completed work owner commit", func(t *testing.T) {
		tx, db, stop := begin(t)
		id := insert(t, db)
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal("commit failed")
		}
		var value string
		if pool.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE id=$1", id).Scan(&value) != nil || value != "callback-test" {
			t.Fatal("committed write missing")
		}
		completed, finish, err := newCallbackDB(tx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := completed.ExecContext(ctx, "SELECT 1"); err != sql.ErrTxDone {
			t.Fatal("completed tx Exec error")
		}
		if rows, err := completed.QueryContext(ctx, "SELECT 1"); rows != nil || err != sql.ErrTxDone {
			t.Fatal("completed tx Query error")
		}
		if err := completed.QueryRowContext(ctx, "SELECT 1").Scan(new(int)); err != sql.ErrTxDone {
			t.Fatal("completed tx Row error")
		}
		if err := finish(); err != nil {
			t.Fatal(err)
		}
	})
	for _, kind := range []string{"row", "partial", "exhausted", "closed"} {
		t.Run("retained "+kind, func(t *testing.T) {
			tx, db, stop := begin(t)
			var row Row
			var rows Rows
			var err error
			if kind == "row" {
				row = db.QueryRowContext(ctx, "SELECT 1")
			} else {
				rows, err = db.QueryContext(ctx, "SELECT generate_series(1,3)")
				if err != nil {
					t.Fatal("query failed")
				}
				switch kind {
				case "partial":
					if !rows.Next() {
						t.Fatal("missing first row")
					}
				case "exhausted":
					for rows.Next() {
					}
					if rows.Err() != nil {
						t.Fatal("iteration error")
					}
				case "closed":
					if rows.Close() != nil {
						t.Fatal("close error")
					}
				}
			}
			err = stop()
			if errors.Is(err, errCallbackUnfinished) != (kind == "row" || kind == "partial") {
				t.Fatal("unfinished status", err)
			}
			value := 42
			if row != nil && row.Scan(&value) != errCallbackClosed {
				t.Fatal("retained Row open")
			}
			if rows != nil && (rows.Next() || rows.Scan(&value) != errCallbackClosed || rows.Err() != errCallbackClosed || rows.Close() != errCallbackClosed) {
				t.Fatal("retained Rows open")
			}
			if value != 42 {
				t.Fatal("closed destination changed")
			}
			if _, err := db.ExecContext(ctx, "SELECT 1"); err != errCallbackClosed {
				t.Fatal("retained DB open")
			}
			if rows, err := db.QueryContext(ctx, "SELECT 1"); rows != nil || err != errCallbackClosed {
				t.Fatal("retained DB query open")
			}
			if db.QueryRowContext(ctx, "SELECT 1").Scan(&value) != errCallbackClosed {
				t.Fatal("retained DB row open")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal("rollback failed")
			}
		})
	}
	for _, kind := range []string{"row", "rows"} {
		t.Run("Scanner panic "+kind, func(t *testing.T) {
			tx, db, stop := begin(t)
			var scan func(...any) error
			if kind == "row" {
				scan = db.QueryRowContext(ctx, "SELECT 1").Scan
			} else {
				rows, err := db.QueryContext(ctx, "SELECT 1")
				if err != nil || !rows.Next() {
					t.Fatal("query failed")
				}
				scan = rows.Scan
			}
			identity := &struct{ value int }{91}
			done := make(chan struct{})
			var caught any
			go func() {
				defer close(done)
				defer func() { caught = recover() }()
				_ = scan(callbackScanFunc(func(any) error { panic(identity) }))
			}()
			callbackWait(t, done)
			if caught != identity {
				t.Fatal("panic identity changed")
			}
			if err := stop(); err != nil {
				t.Fatal("panic cleanup failed")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal("panic rollback failed")
			}
		})
	}
	t.Run("no rows and RawBytes", func(t *testing.T) {
		_, db, _ := begin(t)
		if err := db.QueryRowContext(ctx, "SELECT 1 WHERE false").Scan(new(int)); err != sql.ErrNoRows {
			t.Fatal("no rows lost")
		}
		var bytes sql.RawBytes
		if err := db.QueryRowContext(ctx, "SELECT 'text'").Scan(&bytes); err == nil {
			t.Fatal("RawBytes admitted")
		}
	})
	t.Run("query error", func(t *testing.T) {
		_, db, _ := begin(t)
		rows, err := db.QueryContext(ctx, "SELECT 1/0")
		if rows != nil || err == nil {
			t.Fatal("query error missing")
		}
	})
	t.Run("cancel and drain active PostgreSQL", func(t *testing.T) {
		tx, db, stop := begin(t)
		id := insert(t, db)
		var pid int
		if tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid) != nil {
			t.Fatal("backend probe failed")
		}
		done := make(chan struct{})
		var callErr error
		go func() { defer close(done); _, callErr = db.ExecContext(ctx, "SELECT pg_sleep(20)") }()
		probe, release := context.WithTimeout(ctx, 5*time.Second)
		defer release()
		for {
			var running bool
			if err := pool.QueryRowContext(probe, "SELECT state='active' AND query LIKE '%pg_sleep%' FROM pg_stat_activity WHERE pid=$1", pid).Scan(&running); err != nil {
				t.Fatal("active query probe failed")
			}
			if running {
				break
			}
		}
		if err := stop(); !errors.Is(err, errCallbackUnfinished) {
			t.Fatal("active query not unfinished", err)
		}
		callbackWait(t, done)
		if callErr == nil {
			t.Fatal("cancelled query succeeded")
		}
		if err := tx.Rollback(); err != nil {
			// pgx may close the connection on request cancellation. An error
			// alone (including ErrTxDone) proves no rollback: wait for the
			// actual backend to exit before checking that its write is absent.
			t.Logf("cancelled transaction rollback error type: %T", err)
			ended, cancelEnded := context.WithTimeout(ctx, 5*time.Second)
			defer cancelEnded()
			for {
				var exists bool
				if pool.QueryRowContext(ended, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid).Scan(&exists) != nil {
					t.Fatal("cancelled transaction backend termination not proven")
				}
				if !exists {
					break
				}
			}
		}
		var count int
		if pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != 0 {
			t.Fatal("cancelled transaction write survived rollback")
		}
	})
	t.Run("actual deadlock SQLSTATE40P01", func(t *testing.T) {
		// Precreate only our two test rows, then occupy exactly two sessions.
		var ids [2]int64
		for i := range ids {
			if pool.QueryRowContext(ctx, "INSERT INTO "+table+" (value) VALUES ($1) RETURNING id", "deadlock-test").Scan(&ids[i]) != nil {
				t.Fatal("deadlock row setup failed")
			}
			owned = append(owned, ids[i])
		}
		deadCtx, stopDeadline := context.WithTimeout(ctx, 12*time.Second)
		defer stopDeadline()
		var txs [2]*sql.Tx
		var dbs [2]DBTX
		var stops [2]func() error
		for i := range txs {
			var err error
			txs[i], err = pool.BeginTx(deadCtx, nil)
			if err != nil {
				t.Fatal("deadlock begin failed")
			}
			dbs[i], stops[i], err = newCallbackDB(txs[i])
			if err != nil {
				t.Fatal(err)
			}
			defer func(i int) {
				_ = stops[i]()
				if err := txs[i].Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					t.Error("deadlock cleanup failed")
				}
			}(i)
			if _, err = dbs[i].ExecContext(deadCtx, "UPDATE "+table+" SET value=$2 WHERE id=$1", ids[i], "first-lock"); err != nil {
				t.Fatal("first row lock failed")
			}
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := range txs {
			go func(i int) {
				<-start
				_, err := dbs[i].ExecContext(deadCtx, "UPDATE "+table+" SET value=$2 WHERE id=$1", ids[1-i], "second-lock")
				cleanupErr := stops[i]()
				rollbackErr := txs[i].Rollback()
				if cleanupErr != nil || rollbackErr != nil {
					results <- errors.New("deadlock cleanup failure")
					return
				}
				results <- err
			}(i)
		}
		close(start)
		deadlocks, successes := 0, 0
		for range 2 {
			select {
			case err := <-results:
				var pgerr *pgconn.PgError
				if errors.As(err, &pgerr) && pgerr.Code == "40P01" {
					deadlocks++
				} else if err == nil {
					successes++
				} else {
					t.Fatal("unexpected deadlock participant outcome")
				}
			case <-deadCtx.Done():
				t.Fatal("deadlock deadline exceeded")
			}
		}
		if deadlocks != 1 || successes != 1 {
			t.Fatal("expected one PostgreSQL deadlock victim and one successful statement")
		}
	})
}

func TestCallbackDBConcurrentInvalidation(t *testing.T) {
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	db, stop := callbackControlled(callbackControlledDB{exec: func(ctx context.Context) (sql.Result, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}})
	callDone := make(chan struct{})
	go func() { defer close(callDone); _, _ = db.ExecContext(context.Background(), "SELECT 1") }()
	callbackWait(t, entered)
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- stop() }()
	}
	callbackWait(t, cancelled)
	select {
	case <-results:
		t.Fatal("early invalidation")
	default:
	}
	close(release)
	callbackWait(t, callDone)
	var first error
	for i := 0; i < 8; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, errCallbackUnfinished) {
				t.Fatal(err)
			}
			if i == 0 {
				first = err
			} else if first != err {
				t.Fatal("concurrent cleanup result changed")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("invalidation join deadline")
		}
	}
}

func TestCallbackDBCancellationAndTerminalError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	db, stop := callbackControlled(callbackControlledDB{exec: func(ctx context.Context) (sql.Result, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }})
	done := make(chan error, 1)
	go func() { _, err := db.ExecContext(ctx, "SELECT 1"); done <- err }()
	callbackWait(t, entered)
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal("context error changed", err)
	}
	if err := stop(); err != nil {
		t.Fatal("cancellation retained lease", err)
	}

	terminal := errors.New("terminal SQL error")
	raw := &callbackControlledRows{next: func() bool { return false }, err: terminal}
	db, stop = callbackControlled(callbackControlledDB{query: func(context.Context) (Rows, error) { return raw, nil }})
	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() || rows.Err() != terminal || rows.Err() != terminal {
		t.Fatal("exhausted error changed")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if rows.Err() != errCallbackClosed {
		t.Fatal("old terminal error beats closed state")
	}
}

func TestCallbackDBScannerPanicCloseFailure(t *testing.T) {
	for _, kind := range []string{"row", "rows"} {
		t.Run(kind, func(t *testing.T) {
			value := &struct{ marker int }{5}
			raw := &callbackControlledRows{closeErr: errors.New("private close diagnostic"), scan: func(d ...any) error { return d[0].(sql.Scanner).Scan(int64(1)) }}
			db, stop := callbackControlled(callbackControlledDB{query: func(context.Context) (Rows, error) { return raw, nil }})
			var scan func(...any) error
			if kind == "row" {
				scan = db.QueryRowContext(context.Background(), "SELECT 1").Scan
			} else {
				rows, err := db.QueryContext(context.Background(), "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				scan = rows.Scan
			}
			var caught any
			func() {
				defer func() { caught = recover() }()
				_ = scan(callbackScanFunc(func(any) error { panic(value) }))
			}()
			if caught != value {
				t.Fatal("cleanup replaced panic")
			}
			if err := stop(); !errors.Is(err, errCallbackCleanup) || errors.Is(err, raw.closeErr) || errors.Is(err, errCallbackUnfinished) {
				t.Fatal("cleanup classification", err)
			}
			if raw.closes != 1 {
				t.Fatal("cleanup count", raw.closes)
			}
		})
	}
}
