package operation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/policy"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
)

type completionCodec struct {
	encode   func(any) ([]byte, error)
	validate func([]byte) error
}

func (completionCodec) SchemaDigest() [32]byte {
	return (replayOutputCodec{&replayProbe{}}).SchemaDigest()
}
func (c completionCodec) EncodeCanonical(v any) ([]byte, error) { return c.encode(v) }
func (c completionCodec) ValidateCanonical(v []byte) error      { return c.validate(v) }
func completionDefinition(t *testing.T, codec completionCodec, handler func(context.Context, InvocationContext, replayInput) (Result[any], error)) Definition {
	t.Helper()
	base, p := replayDefinition(t, 1024)
	d, err := Bind(base.Metadata(), replayInputCodec{p}, replayResolver{p}, codec, handler)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func completionUnitTx(t *testing.T) (*invocationConn, *sql.Tx) {
	t.Helper()
	conn := &invocationConn{result: driver.RowsAffected(1)}
	pool := sql.OpenDB(invocationDriver{conn})
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
	return conn, tx
}
func completionClosed(t *testing.T, db DBTX, row Row, rows Rows) {
	t.Helper()
	dest := 991
	if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != errCallbackClosed {
		t.Fatal("codec observed live retained database", err)
	}
	if got, err := db.QueryContext(context.Background(), "SELECT 1"); got != nil || err != errCallbackClosed {
		t.Fatal("retained Query", err)
	}
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&dest); err != errCallbackClosed {
		t.Fatal("retained QueryRow", err)
	}
	if row != nil && row.Scan(&dest) != errCallbackClosed {
		t.Fatal("retained Row not closed")
	}
	if rows != nil && (rows.Next() || rows.Scan(&dest) != errCallbackClosed || rows.Err() != errCallbackClosed || rows.Close() != errCallbackClosed) {
		t.Fatal("retained Rows not closed")
	}
	if dest != 991 {
		t.Fatal("retained Scan mutated destination")
	}
}
func TestCallbackInvocationBeforeCodec(t *testing.T) {
	for _, mode := range []string{"success", "nil-interface", "encode-error", "validate-error", "encode-panic", "validate-panic", "invalid-kind"} {
		t.Run(mode, func(t *testing.T) {
			conn, tx := completionUnitTx(t)
			var db DBTX
			var row Row
			var rows Rows
			sentinel := errors.New("codec failure")
			panicValue := &struct{ value int }{19}
			calls := 0
			encoded := []byte(`"done"`)
			probe := func() {
				calls++
				completionClosed(t, db, row, rows)
				if conn.query != "" {
					t.Fatal("codec reached driver")
				}
			}
			codec := completionCodec{encode: func(v any) ([]byte, error) {
				probe()
				if mode == "nil-interface" && v != nil {
					t.Fatal("nil interface output lost")
				}
				if mode == "encode-error" {
					return nil, sentinel
				}
				if mode == "encode-panic" {
					panic(panicValue)
				}
				return encoded, nil
			}, validate: func([]byte) error {
				probe()
				if mode == "validate-error" {
					return sentinel
				}
				if mode == "validate-panic" {
					panic(panicValue)
				}
				return nil
			}}
			input := InvocationContext{DB: callbackControlledDB{}, Selection: workspacecontext.Selection{Permissions: []string{"original"}}, LockedResources: []policy.Resource{{}}}
			d := completionDefinition(t, codec, func(ctx context.Context, inv InvocationContext, _ replayInput) (Result[any], error) {
				db = inv.DB
				inv.Selection.Permissions[0] = "changed"
				inv.LockedResources[0].Type = "changed"
				row = db.QueryRowContext(ctx, "SELECT 1")
				if err := row.Scan(new(int)); err != nil {
					t.Fatal(err)
				}
				var err error
				rows, err = db.QueryContext(ctx, "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				if err = rows.Close(); err != nil {
					t.Fatal(err)
				}
				conn.query = ""
				kind := ResultSucceeded
				if mode == "invalid-kind" {
					kind = "invalid"
				}
				return Result[any]{Kind: kind}, nil
			})
			var result CachedResult
			var err error
			var caught any
			func() {
				defer func() { caught = recover() }()
				result, err = invokeCallback(context.Background(), d, input, replayInput{}, tx)
			}()
			if input.Selection.Permissions[0] != "original" || input.LockedResources[0].Type != "" {
				t.Fatal("invocation not cloned")
			}
			completionClosed(t, db, row, rows)
			switch mode {
			case "encode-panic", "validate-panic":
				if caught != panicValue {
					t.Fatal("codec panic identity", caught)
				}
			case "encode-error", "validate-error":
				if err != sentinel || result.CanonicalJSON != nil {
					t.Fatal("codec error/output", err)
				}
			case "invalid-kind":
				if err != ErrInvalidDefinition || calls != 0 {
					t.Fatal("invalid kind reached codec", err)
				}
			default:
				if err != nil || caught != nil || calls != 2 || string(result.CanonicalJSON) != `"done"` {
					t.Fatal("completion", err, caught, calls)
				}
				encoded[0] = 'x'
				if result.CanonicalJSON[0] == 'x' {
					t.Fatal("output alias")
				}
			}
		})
	}
}
func TestCallbackInvocationAdmission(t *testing.T) {
	_, tx := completionUnitTx(t)
	calls := 0
	codec := completionCodec{encode: func(any) ([]byte, error) { calls++; return []byte(`null`), nil }, validate: func([]byte) error { calls++; return nil }}
	d := completionDefinition(t, codec, func(context.Context, InvocationContext, replayInput) (Result[any], error) {
		calls++
		return Result[any]{Kind: ResultSucceeded}, nil
	})
	for _, tc := range []struct {
		name string
		ctx  context.Context
		tx   *sql.Tx
		d    Definition
		want error
	}{
		{"nil-context", nil, tx, d, errCallbackContext}, {"nil-tx", context.Background(), nil, d, errNilSQLTx}, {"empty-definition", context.Background(), tx, Definition{}, ErrInvalidDefinition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := invokeCallback(tc.ctx, tc.d, InvocationContext{}, replayInput{}, tc.tx)
			if err != tc.want || got.CanonicalJSON != nil || calls != 0 {
				t.Fatal("invalid admission", err)
			}
		})
	}
	for _, stage := range []string{"handle", "complete"} {
		copy := d
		if stage == "handle" {
			copy.handle = nil
		} else {
			copy.complete = nil
		}
		if _, err := NewRegistry(copy); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatal("registry admitted incomplete stages")
		}
		if _, err := invokeCallback(context.Background(), copy, InvocationContext{}, replayInput{}, tx); err != ErrInvalidDefinition {
			t.Fatal("helper admitted incomplete stages")
		}
	}
	var retained DBTX
	old := d.handle
	d.handle = func(ctx context.Context, inv InvocationContext, value any) (any, error) {
		retained = inv.DB
		return old(ctx, inv, value)
	}
	if _, err := invokeCallback(context.Background(), d, InvocationContext{}, 42, tx); err != ErrInvalidDefinition || calls != 0 {
		t.Fatal("invalid input reached handler/codec", err)
	}
	completionClosed(t, retained, nil, nil)
	d.handle = func(_ context.Context, inv InvocationContext, _ any) (any, error) {
		retained = inv.DB
		return "wrong boxed result", nil
	}
	if _, err := invokeCallback(context.Background(), d, InvocationContext{}, replayInput{}, tx); err != ErrInvalidDefinition || calls != 0 {
		t.Fatal("invalid result reached codec", err)
	}
	completionClosed(t, retained, nil, nil)
	// The old private path still supports interface-typed nil output.
	d = completionDefinition(t, codec, func(context.Context, InvocationContext, replayInput) (Result[any], error) {
		return Result[any]{Kind: ResultSucceeded}, nil
	})
	if result, err := d.invoke(context.Background(), InvocationContext{}, replayInput{}); err != nil || string(result.CanonicalJSON) != "null" {
		t.Fatal("compatibility invoke", err)
	}
}
func TestCallbackInvocationErrorsAndPanic(t *testing.T) {
	for _, mode := range []string{"handler-error", "unfinished", "joined", "panic", "panic-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			_, tx := completionUnitTx(t)
			sentinel := errors.New("handler failure")
			panicValue := &struct{ v int }{31}
			calls := 0
			var db DBTX
			var rows Rows
			codec := completionCodec{encode: func(any) ([]byte, error) { calls++; return []byte(`null`), nil }, validate: func([]byte) error { calls++; return nil }}
			d := completionDefinition(t, codec, func(ctx context.Context, inv InvocationContext, _ replayInput) (Result[any], error) {
				db = inv.DB
				if mode == "unfinished" || mode == "joined" || mode == "panic-cleanup" {
					db.(*callbackDB).state.db = callbackControlledDB{query: func(context.Context) (Rows, error) { return &callbackControlledRows{closeErr: sentinel}, nil }}
					var err error
					rows, err = db.QueryContext(ctx, "SELECT 1")
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "panic" || mode == "panic-cleanup" {
					panic(panicValue)
				}
				if mode == "handler-error" || mode == "joined" {
					return Result[any]{}, sentinel
				}
				return Result[any]{Kind: ResultSucceeded}, nil
			})
			var got CachedResult
			var err error
			var caught any
			func() {
				defer func() { caught = recover() }()
				got, err = invokeCallback(context.Background(), d, InvocationContext{}, replayInput{}, tx)
			}()
			if calls != 0 || got.CanonicalJSON != nil {
				t.Fatal("failure reached codec/output")
			}
			completionClosed(t, db, nil, rows)
			switch mode {
			case "handler-error":
				if err != sentinel {
					t.Fatal("handler identity", err)
				}
			case "unfinished", "joined":
				if !errors.Is(err, errCallbackUnfinished) || !errors.Is(err, errCallbackCleanup) {
					t.Fatal("cleanup identity", err)
				}
				if mode == "joined" && !errors.Is(err, sentinel) {
					t.Fatal("handler error not joined")
				}
			case "panic", "panic-cleanup":
				if caught != panicValue {
					t.Fatal("panic identity", caught)
				}
			}
		})
	}
}
func TestCallbackInvocationWaitsForDrain(t *testing.T) {
	_, tx := completionUnitTx(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	canceled := make(chan struct{})
	scanDone := make(chan struct{})
	finished := make(chan error, 1)
	calls := 0
	raw := &callbackControlledRows{scan: func(...any) error { close(entered); <-release; return nil }}
	codec := completionCodec{encode: func(any) ([]byte, error) { calls++; return []byte(`null`), nil }, validate: func([]byte) error { calls++; return nil }}
	d := completionDefinition(t, codec, func(ctx context.Context, inv InvocationContext, _ replayInput) (Result[any], error) {
		inv.DB.(*callbackDB).state.db = callbackControlledDB{query: func(c context.Context) (Rows, error) { go func() { <-c.Done(); close(canceled) }(); return raw, nil }}
		rows, err := inv.DB.QueryContext(ctx, "SELECT 1")
		if err != nil {
			return Result[any]{}, err
		}
		if !rows.Next() {
			return Result[any]{}, errors.New("missing controlled row")
		}
		go func() { defer close(scanDone); _ = rows.Scan(new(int)) }()
		callbackWait(t, entered)
		return Result[any]{Kind: ResultSucceeded}, nil
	})
	go func() {
		_, err := invokeCallback(context.Background(), d, InvocationContext{}, replayInput{}, tx)
		finished <- err
	}()
	callbackWait(t, canceled)
	select {
	case err := <-finished:
		t.Fatal("returned before scanner drained", err)
	default:
	}
	// Release without sleeps; deferred close owns only this channel's final close.
	release <- struct{}{}
	callbackWait(t, scanDone)
	select {
	case err := <-finished:
		if !errors.Is(err, errCallbackUnfinished) || calls != 0 || raw.closes != 1 {
			t.Fatal("drain/output", err, calls, raw.closes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drain deadline")
	}
}
func TestCallbackInvocationRequiredService(t *testing.T) {
	pool, table := invocationServicePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, mode := range []string{"commit", "unfinished", "panic"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := pool.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal("begin")
			}
			t.Cleanup(func() {
				if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					t.Error("rollback")
				}
			})
			var id int64
			var retained DBTX
			var row Row
			var rows Rows
			calls := 0
			panicValue := &struct{ v int }{71}
			t.Cleanup(func() {
				if id != 0 {
					cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
					defer stop()
					if _, err := pool.ExecContext(cleanup, "DELETE FROM "+table+" WHERE id=$1", id); err != nil {
						t.Error("exact-owned cleanup")
					}
				}
			})
			codec := completionCodec{encode: func(any) ([]byte, error) {
				calls++
				completionClosed(t, retained, row, rows)
				return []byte(`null`), nil
			}, validate: func([]byte) error { calls++; completionClosed(t, retained, row, rows); return nil }}
			d := completionDefinition(t, codec, func(c context.Context, inv InvocationContext, _ replayInput) (Result[any], error) {
				retained = inv.DB
				row = retained.QueryRowContext(c, "INSERT INTO "+table+" (value) VALUES ($1) RETURNING id", "completion-test")
				if err := row.Scan(&id); err != nil {
					return Result[any]{}, err
				}
				var ownerID, callbackID string
				if tx.QueryRowContext(c, "SELECT pg_current_xact_id()::text").Scan(&ownerID) != nil || retained.QueryRowContext(c, "SELECT pg_current_xact_id()::text").Scan(&callbackID) != nil || ownerID != callbackID {
					return Result[any]{}, errors.New("transaction identity")
				}
				var err error
				rows, err = retained.QueryContext(c, "SELECT value FROM "+table+" WHERE id=$1", id)
				if err != nil {
					return Result[any]{}, err
				}
				if mode != "unfinished" {
					if err := rows.Close(); err != nil {
						return Result[any]{}, err
					}
				}
				if mode == "panic" {
					panic(panicValue)
				}
				return Result[any]{Kind: ResultCreated}, nil
			})
			var result CachedResult
			var caught any
			func() {
				defer func() { caught = recover() }()
				result, err = invokeCallback(ctx, d, InvocationContext{}, replayInput{}, tx)
			}()
			completionClosed(t, retained, row, rows)
			var count int
			if pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != 0 {
				t.Fatal("owner transaction escaped before completion")
			}
			if mode == "commit" {
				if err != nil || caught != nil || calls != 2 || result.Kind != ResultCreated {
					t.Fatal("successful completion", err, caught)
				}
				if tx.Commit() != nil {
					t.Fatal("owner commit")
				}
			} else {
				if calls != 0 || result.CanonicalJSON != nil {
					t.Fatal("failure produced output")
				}
				if mode == "unfinished" && !errors.Is(err, errCallbackUnfinished) {
					t.Fatal("unfinished not rejected", err)
				}
				if mode == "panic" && caught != panicValue {
					t.Fatal("panic identity", caught)
				}
				if tx.Rollback() != nil {
					t.Fatal("owner rollback")
				}
			}
			want := 0
			if mode == "commit" {
				want = 1
			}
			if pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count) != nil || count != want {
				t.Fatal("owner commit/rollback persistence")
			}
		})
	}
}
