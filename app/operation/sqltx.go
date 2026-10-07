package operation

import (
	"context"
	"database/sql"
	"errors"
)

var errNilSQLTx = errors.New("invalid invocation transaction")

// sqlTxAdapter only adapts SQL result types on one owner-supplied transaction.
// It must not be exposed as InvocationContext.DB on its own: the future guarded
// callback wrapper must reject transaction control and invalidate retained
// handles. This helper supplies neither those guards nor an executor/SQL sandbox.
// The owner retains all transaction lifecycle responsibility.
type sqlTxAdapter struct {
	tx *sql.Tx
}

var _ DBTX = (*sqlTxAdapter)(nil)

func newSQLTxAdapter(tx *sql.Tx) (DBTX, error) {
	if tx == nil {
		return nil, errNilSQLTx
	}
	return &sqlTxAdapter{tx: tx}, nil
}

func (db *sqlTxAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.tx.ExecContext(ctx, query, args...)
}

func (db *sqlTxAdapter) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := db.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (db *sqlTxAdapter) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return db.tx.QueryRowContext(ctx, query, args...)
}
