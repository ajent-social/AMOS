package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// These scripted driver tests prove Go decisions and SQL call/argument order.
// They do not qualify PostgreSQL syntax, constraints, roles or concurrency.
type step struct {
	match       string
	args        []any
	rows        [][]driver.Value
	err         error
	affected    int64
	affectedErr error
}
type script struct {
	t                                *testing.T
	mu                               sync.Mutex
	steps                            []step
	begins, commits, rollbacks       int
	options                          driver.TxOptions
	beginErr, commitErr, rollbackErr error
}
type connector struct{ s *script }

func (c connector) Connect(context.Context) (driver.Conn, error) { return &connection{s: c.s}, nil }
func (c connector) Driver() driver.Driver                        { return stubDriver{} }

type stubDriver struct{}

func (stubDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused driver open") }

type connection struct{ s *script }

func (c *connection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *connection) Close() error { return nil }
func (c *connection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *connection) BeginTx(_ context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.s.begins++
	c.s.options = o
	return c, c.s.beginErr
}
func (c *connection) Commit() error   { c.s.commits++; return c.s.commitErr }
func (c *connection) Rollback() error { c.s.rollbacks++; return c.s.rollbackErr }
func (c *connection) next(query string, args []driver.NamedValue) step {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if len(c.s.steps) == 0 {
		c.s.t.Fatalf("unexpected SQL: %s", query)
	}
	st := c.s.steps[0]
	c.s.steps = c.s.steps[1:]
	if !strings.Contains(query, st.match) {
		c.s.t.Fatalf("SQL got %q want containing %q", query, st.match)
	}
	if st.args != nil {
		actual := make([]any, len(args))
		for i, a := range args {
			actual[i] = a.Value
		}
		expected := make([]any, len(st.args))
		for i, a := range st.args {
			v, e := driver.DefaultParameterConverter.ConvertValue(a)
			if e != nil {
				c.s.t.Fatal(e)
			}
			expected[i] = v
		}
		if !reflect.DeepEqual(actual, expected) {
			c.s.t.Fatalf("SQL arguments differ: got %v want %v", actual, expected)
		}
	}
	return st
}
func (c *connection) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.next(q, args)
	if s.err != nil {
		return nil, s.err
	}
	return &rows{data: s.rows}, nil
}
func (c *connection) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s := c.next(q, args)
	return result{s.affected, s.affectedErr}, s.err
}

type result struct {
	n   int64
	err error
}

func (r result) LastInsertId() (int64, error) { return 0, errors.New("unused insert id") }
func (r result) RowsAffected() (int64, error) { return r.n, r.err }

type rows struct{ data [][]driver.Value }

func (r *rows) Columns() []string {
	n := 21
	if len(r.data) > 0 {
		n = len(r.data[0])
	}
	out := make([]string, n)
	for i := range out {
		out[i] = "column"
	}
	return out
}
func (r *rows) Close() error { return nil }
func (r *rows) Next(dest []driver.Value) error {
	if len(r.data) == 0 {
		return io.EOF
	}
	copy(dest, r.data[0])
	r.data = r.data[1:]
	return nil
}
func database(t *testing.T, steps ...step) (*sql.DB, *script) {
	t.Helper()
	s := &script{t: t, steps: steps}
	db := sql.OpenDB(connector{s})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
		if len(s.steps) != 0 {
			t.Errorf("%d SQL steps unused", len(s.steps))
		}
	})
	return db, s
}
func transaction(t *testing.T, steps ...step) (*Store, *sql.Tx, *script) {
	t.Helper()
	db, s := database(t, steps...)
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return store, tx, s
}
func id(n byte) identity.ID {
	u := uuid.MustParse("019a1111-2222-7333-8444-555555555500")
	u[15] = n
	return u
}
func scope() operation.Scope {
	return operation.Scope{InstallationID: id(1), ApplicationID: id(2), EnvironmentID: id(3), WorkspaceID: id(4), ActorKind: "person", ActorID: id(5), OperationID: "case.update"}
}
func contract() operation.ReplayContract {
	return operation.ReplayContract{OperationRevision: "v1", DescriptorDigest: [32]byte{1}, InputSchemaDigest: [32]byte{2}, OutputSchemaDigest: [32]byte{3}}
}

var key = operation.KeyDigest{4}
var request = operation.RequestHash{5}

func isolation() step {
	return step{match: "SHOW transaction_isolation", rows: [][]driver.Value{{"read committed"}}}
}
func capacities(actorLimit, actorUsed, workspaceLimit, workspaceUsed int64) []step {
	return []step{{match: actorCapacitySQL, args: actorArgs(scope()), rows: [][]driver.Value{{actorLimit, actorUsed}}}, {match: workspaceCapacitySQL, args: append(actorArgs(scope()), scope().WorkspaceID), rows: [][]driver.Value{{workspaceLimit, workspaceUsed}}}}
}
func stored(completed bool, body []byte) []driver.Value {
	s, c := scope(), contract()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := []driver.Value{id(6).String(), s.InstallationID.String(), s.ApplicationID.String(), s.EnvironmentID.String(), s.ActorKind, s.ActorID.String(), s.WorkspaceID.String(), s.OperationID, append([]byte(nil), key[:]...), append([]byte(nil), request[:]...), c.OperationRevision, append([]byte(nil), c.DescriptorDigest[:]...), append([]byte(nil), c.InputSchemaDigest[:]...), append([]byte(nil), c.OutputSchemaDigest[:]...), int64(100), "pending", nil, nil, nil, now, nil}
	if completed {
		hash := sha256.Sum256(body)
		out[14] = int64(len(body))
		out[15] = "completed"
		out[16] = "succeeded"
		out[17] = append([]byte(nil), body...)
		out[18] = hash[:]
		out[20] = now
	}
	return out
}
func claimSteps(row []driver.Value) []step {
	out := append([]step{isolation()}, capacities(1000, 200, 500, 100)...)
	last := step{match: selectInvocation, args: append(scopeArgs(scope()), key[:])}
	if row != nil {
		last.rows = [][]driver.Value{row}
	}
	return append(out, last)
}
func claim(store *Store, tx *sql.Tx) (operation.Invocation, operation.ClaimKind, error) {
	return store.ClaimTx(context.Background(), tx, id(9), scope(), key, request, contract(), 100)
}

func TestClaimValidationBeforeSQL(t *testing.T) {
	type input struct {
		id  identity.ID
		s   operation.Scope
		k   operation.KeyDigest
		r   operation.RequestHash
		c   operation.ReplayContract
		max int
	}
	for name, change := range map[string]func(*input){
		"nil_id": func(v *input) { v.id = uuid.Nil }, "v4_id": func(v *input) { v.id = uuid.New() }, "variant": func(v *input) { v.id[8] = 0 },
		"installation": func(v *input) { v.s.InstallationID = uuid.Nil }, "application": func(v *input) { v.s.ApplicationID = uuid.Nil }, "environment": func(v *input) { v.s.EnvironmentID = uuid.Nil }, "workspace": func(v *input) { v.s.WorkspaceID = uuid.Nil }, "actor": func(v *input) { v.s.ActorID = uuid.Nil }, "actor_kind": func(v *input) { v.s.ActorKind = "service" },
		"operation_short": func(v *input) { v.s.OperationID = "ab" }, "operation_long": func(v *input) { v.s.OperationID = strings.Repeat("a", 121) }, "operation_grammar": func(v *input) { v.s.OperationID = "Case..update" },
		"key": func(v *input) { v.k = operation.KeyDigest{} }, "request": func(v *input) { v.r = operation.RequestHash{} }, "descriptor": func(v *input) { v.c.DescriptorDigest = [32]byte{} }, "input": func(v *input) { v.c.InputSchemaDigest = [32]byte{} }, "output": func(v *input) { v.c.OutputSchemaDigest = [32]byte{} },
		"revision_empty": func(v *input) { v.c.OperationRevision = "" }, "revision_space": func(v *input) { v.c.OperationRevision = " v1" }, "revision_control": func(v *input) { v.c.OperationRevision = "v\x001" }, "revision_utf8": func(v *input) { v.c.OperationRevision = "v\xff" }, "revision_long": func(v *input) { v.c.OperationRevision = strings.Repeat("v", 161) }, "maximum_zero": func(v *input) { v.max = 0 }, "maximum_large": func(v *input) { v.max = 65537 },
	} {
		t.Run(name, func(t *testing.T) {
			st, tx, _ := transaction(t)
			v := input{id(9), scope(), key, request, contract(), 100}
			change(&v)
			got, kind, err := st.ClaimTx(context.Background(), tx, v.id, v.s, v.k, v.r, v.c, v.max)
			if err != ErrInvalid || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
				t.Fatalf("invalid input disclosed result: %v %v %v", got, kind, err)
			}
		})
	}
}

func TestClaimReplayAndConflict(t *testing.T) {
	for _, name := range []string{"replay", "request", "revision", "descriptor", "input", "output"} {
		t.Run(name, func(t *testing.T) {
			body := []byte(" {\"answer\": 42} \n")
			source := stored(true, body)
			st, tx, _ := transaction(t, claimSteps(source)...)
			r, c := request, contract()
			switch name {
			case "request":
				r[1] = 1
			case "revision":
				c.OperationRevision = "v2"
			case "descriptor":
				c.DescriptorDigest[1] = 1
			case "input":
				c.InputSchemaDigest[1] = 1
			case "output":
				c.OutputSchemaDigest[1] = 1
			}
			got, kind, err := st.ClaimTx(context.Background(), tx, id(9), scope(), key, r, c, 1)
			if err != nil {
				t.Fatal(err)
			}
			if name != "replay" {
				if kind != operation.ClaimConflict || !reflect.DeepEqual(got, operation.Invocation{}) {
					t.Fatal("conflict disclosed output")
				}
				return
			}
			if kind != operation.ClaimReplay || got.ID != id(6) || string(got.Result.CanonicalJSON) != string(body) || got.ResultSHA256 != sha256.Sum256(body) {
				t.Fatal("incorrect replay")
			}
			source[17].([]byte)[0] = '!'
			if string(got.Result.CanonicalJSON) != string(body) {
				t.Fatal("replay aliases driver bytes")
			}
		})
	}
}

func TestClaimCorruptionPrecedesConflict(t *testing.T) {
	for name, mutate := range map[string]func([]driver.Value){
		"pending": func(v []driver.Value) { v[15] = "pending" }, "state": func(v []driver.Value) { v[15] = "unknown" }, "kind": func(v []driver.Value) { v[16] = "bad" }, "null_kind": func(v []driver.Value) { v[16] = nil }, "bad_digest": func(v []driver.Value) { v[18] = make([]byte, 32) }, "short_digest": func(v []driver.Value) { v[18] = []byte{1} }, "wrong_size": func(v []driver.Value) { v[14] = int64(50) }, "zero_size": func(v []driver.Value) { v[14] = int64(0) }, "oversized": func(v []driver.Value) { v[14] = int64(65537) }, "invalid_json": func(v []driver.Value) { v[17] = []byte("??") }, "invalid_utf8": func(v []driver.Value) { v[17] = []byte{'"', 255, '"'}; v[14] = int64(3) }, "missing_created": func(v []driver.Value) { v[19] = nil }, "missing_completed": func(v []driver.Value) { v[20] = nil }, "id": func(v []driver.Value) { v[0] = uuid.Nil.String() }, "scope": func(v []driver.Value) { v[6] = id(99).String() }, "key": func(v []driver.Value) { v[8] = make([]byte, 32) }, "request": func(v []driver.Value) { v[9] = make([]byte, 32) }, "short_descriptor": func(v []driver.Value) { v[11] = []byte{1} }, "zero_input": func(v []driver.Value) { v[12] = make([]byte, 32) }, "revision": func(v []driver.Value) { v[10] = "bad\n" },
	} {
		t.Run(name, func(t *testing.T) {
			v := stored(true, []byte(`{"ok":true}`))
			mutate(v)
			st, tx, _ := transaction(t, claimSteps(v)...)
			different := request
			different[1] = 9
			got, kind, err := st.ClaimTx(context.Background(), tx, id(9), scope(), key, different, contract(), 100)
			if err != ErrUnavailable || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
				t.Fatalf("corruption not closed: %v %v", kind, err)
			}
		})
	}
}

func TestClaimNewAndCapacity(t *testing.T) {
	for _, name := range []string{"new", "actor_exhausted", "workspace_exhausted", "bigint_boundary"} {
		t.Run(name, func(t *testing.T) {
			a, u, w, v := int64(1000), int64(100), int64(500), int64(100)
			switch name {
			case "actor_exhausted":
				a = 150
				w = 150
			case "workspace_exhausted":
				w = 150
			case "bigint_boundary":
				a = math.MaxInt64
				u = math.MaxInt64 - 100
				w = math.MaxInt64
				v = math.MaxInt64 - 100
			}
			steps := append([]step{isolation()}, capacities(a, u, w, v)...)
			steps = append(steps, step{match: selectInvocation, args: append(scopeArgs(scope()), key[:])})
			want := operation.ClaimCapacityUnavailable
			if name == "new" || name == "bigint_boundary" {
				want = operation.ClaimNew
				c := contract()
				args := append([]any{id(9)}, scopeArgs(scope())...)
				args = append(args, key[:], request[:], c.OperationRevision, c.DescriptorDigest[:], c.InputSchemaDigest[:], c.OutputSchemaDigest[:], int64(100))
				steps = append(steps, step{match: updateActorSQL, args: append(actorArgs(scope()), u+100), affected: 1}, step{match: updateWorkspaceSQL, args: append(append(actorArgs(scope()), scope().WorkspaceID), v+100), affected: 1}, step{match: insertInvocation, args: args, affected: 1})
			}
			st, tx, _ := transaction(t, steps...)
			got, kind, err := claim(st, tx)
			if err != nil || kind != want {
				t.Fatalf("%v %v", kind, err)
			}
			if kind == operation.ClaimNew {
				if got.ID != id(9) || got.Scope != scope() || got.Result.CanonicalJSON != nil {
					t.Fatal("invalid new claim")
				}
			} else if !reflect.DeepEqual(got, operation.Invocation{}) {
				t.Fatal("capacity returned claim")
			}
		})
	}
}

func TestClaimCapacityCorruption(t *testing.T) {
	for _, v := range [][4]int64{{0, 0, 1, 0}, {100, -1, 50, 0}, {100, 101, 50, 0}, {100, 0, 0, 0}, {100, 0, 101, 0}, {100, 0, 50, -1}, {100, 99, 50, 51}, {100, 1, 50, 2}} {
		t.Run("bounds", func(t *testing.T) {
			st, tx, _ := transaction(t, append([]step{isolation()}, capacities(v[0], v[1], v[2], v[3])...)...)
			got, kind, err := claim(st, tx)
			if err != ErrUnavailable || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
				t.Fatal("inconsistent capacity accepted")
			}
		})
	}
}

func TestClaimSQLFailuresAreFinite(t *testing.T) {
	base := claimSteps(nil)
	base = append(base, step{match: updateActorSQL, affected: 1}, step{match: updateWorkspaceSQL, affected: 1}, step{match: insertInvocation, affected: 1})
	for i := range base {
		t.Run("step", func(t *testing.T) {
			steps := append([]step(nil), base[:i+1]...)
			steps[i].err = errors.New("private database diagnostic")
			st, tx, _ := transaction(t, steps...)
			got, kind, err := claim(st, tx)
			if err != ErrUnavailable || err.Error() == steps[i].err.Error() || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
				t.Fatal("SQL failure escaped or disclosed")
			}
		})
	}
	for _, which := range []string{"actor", "workspace"} {
		t.Run("missing_"+which, func(t *testing.T) {
			steps := []step{isolation(), {match: actorCapacitySQL}}
			if which == "workspace" {
				steps = append([]step{isolation()}, capacities(1000, 100, 500, 100)...)
				steps[2].rows = nil
			}
			st, tx, _ := transaction(t, steps...)
			_, _, err := claim(st, tx)
			if err != ErrUnavailable {
				t.Fatal(err)
			}
		})
	}
	for _, affected := range []int64{0, 2} {
		t.Run("affected", func(t *testing.T) {
			steps := append(claimSteps(nil), step{match: updateActorSQL, affected: affected})
			st, tx, _ := transaction(t, steps...)
			_, _, err := claim(st, tx)
			if err != ErrUnavailable {
				t.Fatal(err)
			}
		})
	}
}

func TestCompleteExactBytesAndShrink(t *testing.T) {
	for _, body := range []string{`{"answer":42}`, `"\u0000"`, `1e1000000`, `null`, " \n[1,2] "} {
		t.Run("bytes", func(t *testing.T) {
			pending := stored(false, nil)
			digest := sha256.Sum256([]byte(body))
			steps := []step{isolation(), {match: selectInvocation + ` WHERE id=$1 FOR UPDATE`, args: []any{id(6)}, rows: [][]driver.Value{pending}}}
			steps = append(steps, capacities(1000, 200, 500, 100)...)
			n := int64(len(body))
			steps = append(steps, step{match: updateActorSQL, args: append(actorArgs(scope()), 100+n), affected: 1}, step{match: updateWorkspaceSQL, args: append(append(actorArgs(scope()), scope().WorkspaceID), n), affected: 1}, step{match: completeInvocation, args: []any{id(6), "succeeded", []byte(body), digest[:], n}, affected: 1})
			st, tx, _ := transaction(t, steps...)
			if err := st.CompleteTx(context.Background(), tx, id(6), operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(body)}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompleteRejectsInputBeforeSQL(t *testing.T) {
	for name, r := range map[string]operation.CachedResult{"empty": {Kind: operation.ResultSucceeded}, "kind": {Kind: "invalid", CanonicalJSON: []byte(`{}`)}, "malformed": {Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{`)}, "utf8": {Kind: operation.ResultSucceeded, CanonicalJSON: []byte{'"', 255, '"'}}, "large": {Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`"` + strings.Repeat("a", 65535) + `"`)}} {
		t.Run(name, func(t *testing.T) {
			st, tx, _ := transaction(t)
			if err := st.CompleteTx(context.Background(), tx, id(6), r); err != ErrInvalid {
				t.Fatal(err)
			}
		})
	}
	for _, kind := range []operation.ResultKind{operation.ResultSucceeded, operation.ResultCreated, operation.ResultAccepted, operation.ResultNoContent} {
		if !validResult(operation.CachedResult{Kind: kind, CanonicalJSON: []byte("null")}) {
			t.Errorf("rejected finite kind %s", kind)
		}
	}
	if !validResult(operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`"` + strings.Repeat("a", 65534) + `"`)}) {
		t.Fatal("rejected exact upper bound")
	}
}

func TestCompleteStoredFailures(t *testing.T) {
	for _, name := range []string{"missing", "completed", "oversized", "pending_body", "pending_digest", "pending_kind", "pending_time", "bad_state", "bad_id", "capacity"} {
		t.Run(name, func(t *testing.T) {
			v := stored(false, nil)
			want := ErrUnavailable
			switch name {
			case "missing":
				v = nil
				want = ErrInvalid
			case "completed":
				v = stored(true, []byte(`{}`))
				want = ErrInvalid
			case "oversized":
				v[14] = int64(1)
				want = ErrInvalid
			case "pending_body":
				v[17] = []byte(`{}`)
			case "pending_digest":
				v[18] = make([]byte, 32)
			case "pending_kind":
				v[16] = "succeeded"
			case "pending_time":
				v[20] = time.Now()
			case "bad_state":
				v[15] = "bad"
				want = ErrInvalid
			case "bad_id":
				v[0] = uuid.Nil.String()
			}
			row := step{match: selectInvocation}
			if v != nil {
				row.rows = [][]driver.Value{v}
			}
			steps := []step{isolation(), row}
			if name == "capacity" {
				steps = append(steps, capacities(1000, 50, 500, 50)...)
			}
			st, tx, _ := transaction(t, steps...)
			err := st.CompleteTx(context.Background(), tx, id(6), operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{}`)})
			if err != want {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}

func TestTransactionAdmission(t *testing.T) {
	for _, name := range []string{"nil_store", "nil_context", "nil_tx", "canceled", "isolation", "isolation_error"} {
		t.Run(name, func(t *testing.T) {
			steps := []step{}
			if name == "isolation" {
				steps = append(steps, step{match: "SHOW transaction_isolation", rows: [][]driver.Value{{"serializable"}}})
			}
			if name == "isolation_error" {
				steps = append(steps, step{match: "SHOW transaction_isolation", err: errors.New("private")})
			}
			st, tx, _ := transaction(t, steps...)
			ctx := context.Background()
			want := ErrInvalid
			switch name {
			case "nil_store":
				st = nil
			case "nil_context":
				ctx = nil
			case "nil_tx":
				tx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = ErrUnavailable
			case "isolation_error":
				want = ErrUnavailable
			}
			_, _, err := st.ClaimTx(ctx, tx, id(9), scope(), key, request, contract(), 100)
			if err != want {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}

type retainedRunner struct {
	calls   int
	options *sql.TxOptions
	tx      *sql.Tx
	err     error
}

func (r *retainedRunner) WithTx(_ context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	r.calls++
	r.options = opts
	if r.err != nil {
		return r.err
	}
	return fn(r.tx)
}
func TestConstructorsAndRetainedRunner(t *testing.T) {
	if _, err := New(nil); err != ErrInvalid {
		t.Fatal(err)
	}
	var typedNil *retainedRunner
	for _, r := range []storage.TxRunner{nil, typedNil} {
		if _, err := NewWithRunner(r); err != ErrInvalid {
			t.Fatal("nil runner accepted")
		}
	}
	_, tx, _ := transaction(t)
	r := &retainedRunner{tx: tx}
	st, err := NewWithRunner(r)
	if err != nil || st.runner != r {
		t.Fatal("runner not retained")
	}
	sentinel := errors.New("callback failure")
	err = st.WithTx(context.Background(), func(got *sql.Tx) error {
		if got != tx {
			t.Fatal("transaction replaced")
		}
		return sentinel
	})
	if err != sentinel || r.calls != 1 || r.options.Isolation != sql.LevelReadCommitted || r.options.ReadOnly {
		t.Fatal("delegation changed")
	}
	r.err = errors.New("private runner diagnostic")
	if err := st.WithTx(context.Background(), func(*sql.Tx) error { t.Fatal("callback after begin failure"); return nil }); err != ErrUnavailable {
		t.Fatal(err)
	}
	var nilContext context.Context
	if err := st.WithTx(nilContext, func(*sql.Tx) error { return nil }); err != ErrInvalid {
		t.Fatal(err)
	}
	if err := st.WithTx(context.Background(), nil); err != ErrInvalid {
		t.Fatal(err)
	}
}
func TestLegacyTransactionOwnership(t *testing.T) {
	for _, name := range []string{"commit", "callback", "panic", "begin", "commit_failure", "rollback_failure"} {
		t.Run(name, func(t *testing.T) {
			db, state := database(t)
			st, err := New(db)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("callback failure")
			switch name {
			case "begin":
				state.beginErr = errors.New("private begin")
			case "commit_failure":
				state.commitErr = errors.New("private commit")
			case "rollback_failure":
				state.rollbackErr = errors.New("private rollback")
			}
			called := 0
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				err = st.WithTx(context.Background(), func(*sql.Tx) error {
					called++
					if name == "panic" {
						panic(sentinel)
					}
					if name == "callback" || name == "rollback_failure" {
						return sentinel
					}
					return nil
				})
			}()
			if state.begins != 1 || state.options.Isolation != driver.IsolationLevel(sql.LevelReadCommitted) {
				t.Fatal("wrong transaction")
			}
			switch name {
			case "commit":
				if err != nil || state.commits != 1 || state.rollbacks != 0 {
					t.Fatal("commit lifecycle")
				}
			case "callback":
				if err != sentinel || state.rollbacks != 1 || state.commits != 0 {
					t.Fatal("callback lifecycle")
				}
			case "panic":
				if panicValue != sentinel || state.rollbacks != 1 || state.commits != 0 {
					t.Fatal("panic lifecycle")
				}
			case "begin":
				if err != ErrUnavailable || called != 0 {
					t.Fatal("begin failure")
				}
			case "commit_failure":
				if err != ErrUnavailable || state.commits != 1 {
					t.Fatal("commit failure")
				}
			case "rollback_failure":
				if err != ErrUnavailable || state.rollbacks != 1 {
					t.Fatal("rollback failure")
				}
			}
			if e := db.PingContext(context.Background()); e != nil {
				t.Fatal("store closed caller pool")
			}
		})
	}
}

type alteredRunner struct {
	tx       *sql.Tx
	mode     string
	received error
}

func (r *alteredRunner) WithTx(_ context.Context, _ *sql.TxOptions, fn func(*sql.Tx) error) error {
	r.received = fn(r.tx)
	switch r.mode {
	case "joined":
		return errors.Join(r.received, errors.New("private rollback"))
	case "swallowed":
		return nil
	case "replaced":
		return errors.New("private runner")
	default:
		return r.received
	}
}

type nonComparableError []string

func (nonComparableError) Error() string { return "callback" }
func TestCallbackFailureIdentity(t *testing.T) {
	for _, mode := range []string{"unchanged", "joined", "swallowed", "replaced", "nil_transaction"} {
		t.Run(mode, func(t *testing.T) {
			_, tx, _ := transaction(t)
			r := &alteredRunner{tx: tx, mode: mode}
			if mode == "nil_transaction" {
				r.tx = nil
			}
			st, err := NewWithRunner(r)
			if err != nil {
				t.Fatal(err)
			}
			original := nonComparableError{"do not wrap"}
			called := false
			err = st.WithTx(context.Background(), func(*sql.Tx) error { called = true; return original })
			if mode == "unchanged" {
				v, ok := err.(nonComparableError)
				if !ok || &v[0] != &original[0] {
					t.Fatal("callback identity lost")
				}
			} else if err != ErrUnavailable {
				t.Fatal("runner failure escaped")
			}
			if mode == "nil_transaction" && called {
				t.Fatal("nil transaction callback called")
			}
			if _, ok := r.received.(*callbackFailure); !ok {
				t.Fatal("runner did not receive private unique wrapper")
			}
		})
	}
}
func TestCompleteSQLFailuresAreFinite(t *testing.T) {
	base := []step{isolation(), {match: selectInvocation, rows: [][]driver.Value{stored(false, nil)}}}
	base = append(base, capacities(1000, 200, 500, 100)...)
	base = append(base, step{match: updateActorSQL, affected: 1}, step{match: updateWorkspaceSQL, affected: 1}, step{match: completeInvocation, affected: 1})
	for i := range base {
		t.Run("step", func(t *testing.T) {
			steps := append([]step(nil), base[:i+1]...)
			steps[i].err = errors.New("private database diagnostic")
			st, tx, _ := transaction(t, steps...)
			if err := st.CompleteTx(context.Background(), tx, id(6), operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{}`)}); err != ErrUnavailable {
				t.Fatal("SQL diagnostics escaped")
			}
		})
	}
}
