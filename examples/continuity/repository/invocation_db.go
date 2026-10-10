package repository

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/ajent-social/amos/app/operation"
)

// NewWithDBTX retains exactly the supplied database interface and copied scope.
// The caller must supply its current invocation DBTX and serialize calls. Scope
// is only a selector; construction establishes neither authority nor a guarded
// lifetime. Results remain provisional until the caller commits successfully.
func NewWithDBTX(db operation.DBTX, scope Scope) (*Repository, error) {
	if nilDBTX(db) || !validScope(scope) {
		return nil, ErrInvalid
	}
	return &Repository{tx: db, scope: scope}, nil
}

func nilDBTX(db operation.DBTX) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// transactionDB adapts the legacy constructor without exposing its transaction
// or changing the caller's transaction lifetime or ownership.
type transactionDB struct{ tx *sql.Tx }

func (d transactionDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.tx.ExecContext(ctx, query, args...)
}

func (d transactionDB) QueryContext(ctx context.Context, query string, args ...any) (operation.Rows, error) {
	return d.tx.QueryContext(ctx, query, args...)
}

func (d transactionDB) QueryRowContext(ctx context.Context, query string, args ...any) operation.Row {
	return d.tx.QueryRowContext(ctx, query, args...)
}
