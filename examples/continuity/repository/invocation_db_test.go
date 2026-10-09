package repository

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/google/uuid"
)

type invocationCall struct {
	ctx   context.Context
	query string
	args  []any
}
type invocationDB struct {
	calls []invocationCall
	row   operation.Row
	rows  operation.Rows
	err   error
}

func (d *invocationDB) record(ctx context.Context, query string, args []any) {
	d.calls = append(d.calls, invocationCall{ctx, query, append([]any(nil), args...)})
}
func (d *invocationDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.record(ctx, query, args)
	return invocationResult(1), d.err
}
func (d *invocationDB) QueryContext(ctx context.Context, query string, args ...any) (operation.Rows, error) {
	d.record(ctx, query, args)
	return d.rows, d.err
}
func (d *invocationDB) QueryRowContext(ctx context.Context, query string, args ...any) operation.Row {
	d.record(ctx, query, args)
	return d.row
}

type invocationResult int64

func (r invocationResult) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (r invocationResult) RowsAffected() (int64, error) { return int64(r), nil }

type invocationRow func(...any) error

func (r invocationRow) Scan(v ...any) error { return r(v...) }

type invocationRows struct {
	next                      bool
	scanErr, rowErr, closeErr error
	closed                    bool
}

func (r *invocationRows) Next() bool          { next := r.next; r.next = false; return next }
func (r *invocationRows) Scan(v ...any) error { *(v[0].(*string)) = testID; return r.scanErr }
func (r *invocationRows) Err() error          { return r.rowErr }
func (r *invocationRows) Close() error        { r.closed = true; return r.closeErr }

// Named nilable implementations can satisfy DBTX without being pointers.
type invocationMap map[string]string

func (invocationMap) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("unexpected I/O")
}
func (invocationMap) QueryContext(context.Context, string, ...any) (operation.Rows, error) {
	panic("unexpected I/O")
}
func (invocationMap) QueryRowContext(context.Context, string, ...any) operation.Row {
	panic("unexpected I/O")
}

func invocationScope() Scope {
	return Scope{uuid.MustParse(testID), uuid.MustParse("01900000-0000-7000-8000-000000000002"), uuid.MustParse("01900000-0000-7000-8000-000000000003"), uuid.MustParse("01900000-0000-7000-8000-000000000004")}
}
func TestInvocationConstructor(t *testing.T) {
	for name, db := range map[string]operation.DBTX{"nil": nil, "pointer": (*invocationDB)(nil), "map": invocationMap(nil)} {
		t.Run(name, func(t *testing.T) {
			if r, err := NewWithDBTX(db, invocationScope()); r != nil || err != ErrInvalid {
				t.Fatal("nil interface admitted")
			}
		})
	}
	db := &invocationDB{}
	if r, err := NewWithDBTX(db, Scope{}); r != nil || err != ErrInvalid || len(db.calls) != 0 {
		t.Fatal("scope validation performed I/O or admitted invalid scope")
	}
	scope := invocationScope()
	r, err := NewWithDBTX(db, scope)
	if err != nil || r.tx != db || len(db.calls) != 0 {
		t.Fatal("construction replaced interface or performed I/O")
	}
	scope.WorkspaceID = uuid.Nil
	if r.scope != invocationScope() {
		t.Fatal("scope not copied")
	}
	if p, e := r.Property(context.Background(), "invalid"); p != (Property{}) || e != ErrInvalid || len(db.calls) != 0 {
		t.Fatal("invalid selector performed I/O")
	}
}

func TestInvocationQueryForwarding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := &invocationDB{row: invocationRow(func(v ...any) error {
		*(v[0].(*string)) = testID
		*(v[1].(*string)) = "Procedure"
		*(v[2].(*string)) = "Original plain text"
		*(v[3].(*int64)) = 1
		return nil
	})}
	r, err := NewWithDBTX(db, invocationScope())
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Procedure(ctx, testID)
	if err != nil || p.ID != testID || p.Revision != 1 || len(db.calls) != 1 {
		t.Fatal("valid read not forwarded")
	}
	wantQuery := `SELECT id,title,body,revision FROM public.continuity_procedures WHERE ` + scoped + ` AND id=$5`
	call := db.calls[0]
	wantArgs := []any{invocationScope().InstallationID, invocationScope().ApplicationID, invocationScope().EnvironmentID, invocationScope().WorkspaceID, testID}
	if call.ctx != ctx || call.query != wantQuery || !reflect.DeepEqual(call.args, wantArgs) {
		t.Fatal("read changed context, scope, query or selector")
	}
	for name, failure := range map[string]error{"invalidated": errors.New("guard closed"), "missing": sql.ErrNoRows} {
		t.Run(name, func(t *testing.T) {
			db.row = invocationRow(func(v ...any) error { *(v[0].(*string)) = testID; return failure })
			p, e := r.Procedure(ctx, testID)
			want := ErrUnavailable
			if errors.Is(failure, sql.ErrNoRows) {
				want = ErrNotFound
			}
			if p != (domain.Procedure{}) || e != want {
				t.Fatal("scan failure disclosed provisional output")
			}
		})
	}
}

func TestInvocationRowsFailure(t *testing.T) {
	failure := errors.New("guard closed")
	for _, mode := range []string{"query", "scan", "rows", "close"} {
		t.Run(mode, func(t *testing.T) {
			rows := &invocationRows{next: true}
			db := &invocationDB{rows: rows}
			switch mode {
			case "query":
				db.err = failure
			case "scan":
				rows.scanErr = failure
			case "rows":
				rows.rowErr = failure
			case "close":
				rows.closeErr = failure
			}
			r, e := NewWithDBTX(db, invocationScope())
			if e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			values, e := r.Procedures(ctx, "", 10)
			if values != nil || e != ErrUnavailable || len(db.calls) != 1 || (mode != "query" && !rows.closed) {
				t.Fatal("row failure not closed and discarded")
			}
			call := db.calls[0]
			want := []any{invocationScope().InstallationID, invocationScope().ApplicationID, invocationScope().EnvironmentID, invocationScope().WorkspaceID, "", 10}
			if call.ctx != ctx || !strings.Contains(call.query, scoped) || !reflect.DeepEqual(call.args, want) {
				t.Fatal("list changed query context or scoped arguments")
			}
		})
	}
}

func TestInvocationMutationForwarding(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failWrite], func(t *testing.T) {
			db := &invocationDB{row: invocationRow(func(v ...any) error {
				*(v[0].(*string)) = testID
				*(v[1].(*string)) = "Procedure"
				*(v[2].(*string)) = "Old"
				*(v[3].(*int64)) = 1
				return nil
			})}
			if failWrite {
				db.err = errors.New("guard invalidated")
			}
			r, e := NewWithDBTX(db, invocationScope())
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := r.EditProcedure(ctx, testID, 1, "New", testID)
			if failWrite {
				if e != ErrUnavailable || p != (domain.Procedure{}) || len(db.calls) != 2 {
					t.Fatal("write failure disclosed success")
				}
			} else if e != nil || p.Revision != 2 || p.Body != "New" || len(db.calls) != 3 {
				t.Fatal("mutation did not use retained interface")
			}
			for _, call := range db.calls {
				if call.ctx != ctx || !reflect.DeepEqual(call.args[:4], []any{invocationScope().InstallationID, invocationScope().ApplicationID, invocationScope().EnvironmentID, invocationScope().WorkspaceID}) {
					t.Fatal("mutation changed scope or context")
				}
			}
			if !strings.HasSuffix(db.calls[0].query, " FOR UPDATE") {
				t.Fatal("lock query changed")
			}
			wantQuery := `UPDATE public.continuity_procedures SET body=$7,revision=$8,updated_at=transaction_timestamp() WHERE ` + scoped + ` AND id=$5 AND revision=$6`
			if db.calls[1].query != wantQuery || !reflect.DeepEqual(db.calls[1].args[4:], []any{testID, int64(1), "New", int64(2)}) {
				t.Fatal("mutation SQL or revision arguments changed")
			}
			if !failWrite && (!strings.HasPrefix(db.calls[2].query, "INSERT INTO public.continuity_activity ") || !reflect.DeepEqual(db.calls[2].args[5:], []any{testID, "procedure.changed", testID, int64(2)})) {
				t.Fatal("activity not forwarded")
			}
		})
	}
}
