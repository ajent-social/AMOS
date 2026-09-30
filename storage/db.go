// Package storage owns the PostgreSQL pool used by an AMOS application.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

var (
	// ErrInvalidConfig means a PostgreSQL URL is malformed or incomplete.
	ErrInvalidConfig = errors.New("invalid PostgreSQL configuration")
	// ErrUnavailable means PostgreSQL could not be reached during construction.
	ErrUnavailable = errors.New("PostgreSQL is unavailable")
	// ErrClosed means an operation was attempted on a nil or closed handle.
	ErrClosed = errors.New("PostgreSQL handle is closed")
	// ErrTransaction means a transaction could not be started or committed.
	ErrTransaction = errors.New("PostgreSQL transaction failed")
	// ErrClose means one or more PostgreSQL pools could not close cleanly.
	ErrClose = errors.New("PostgreSQL pool failed to close cleanly")
)

// DB is the application-owned PostgreSQL connection pool. It must be closed
// when its application lifecycle ends.
type DB struct {
	pool          *sql.DB
	migrationPool *sql.DB
	closeOnce     sync.Once
	closeErr      error
	closed        atomic.Bool
}

// Open validates a PostgreSQL URL, opens a pool, and pings it using ctx. Errors
// intentionally omit the input URL because it can contain credentials.
func Open(ctx context.Context, dsn string) (*DB, error) {
	if ctx == nil || dsn == "" {
		return nil, ErrInvalidConfig
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.Path == "" || u.Path == "/" {
		return nil, ErrInvalidConfig
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	pool := openPool(config)
	migrationConfig := config.Copy()
	// Migration sources are SQL scripts. Keep simple-protocol execution isolated
	// to the migration pool so application queries retain pgx's normal mode.
	migrationConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	migrationPool := openPool(migrationConfig)
	if err := pool.PingContext(ctx); err != nil {
		closeErr := closePools(pool, migrationPool)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if closeErr != nil {
			return nil, errors.Join(ErrUnavailable, closeErr)
		}
		return nil, ErrUnavailable
	}
	if err := migrationPool.PingContext(ctx); err != nil {
		closeErr := closePools(pool, migrationPool)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if closeErr != nil {
			return nil, errors.Join(ErrUnavailable, closeErr)
		}
		return nil, ErrUnavailable
	}
	return &DB{pool: pool, migrationPool: migrationPool}, nil
}

func openPool(config *pgx.ConnConfig) *sql.DB {
	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(10)
	return pool
}

func closePools(pools ...*sql.DB) error {
	var closeFailed bool
	for _, pool := range pools {
		if pool != nil && pool.Close() != nil {
			closeFailed = true
		}
	}
	if closeFailed {
		return ErrClose
	}
	return nil
}

// Close releases all connections. It is safe to call more than once.
func (db *DB) Close() error {
	if db == nil || db.pool == nil || db.migrationPool == nil {
		return nil
	}
	db.closeOnce.Do(func() {
		db.closed.Store(true)
		db.closeErr = closePools(db.pool, db.migrationPool)
	})
	return db.closeErr
}

// WithTx runs fn in a context-bound transaction and commits only if fn
// succeeds. A failed callback is returned unchanged after rollback.
func (db *DB) WithTx(ctx context.Context, options *sql.TxOptions, fn func(*sql.Tx) error) (result error) {
	if db == nil || db.pool == nil || db.migrationPool == nil || db.closed.Load() {
		return ErrClosed
	}
	if ctx == nil || fn == nil {
		return ErrInvalidConfig
	}
	tx, err := db.pool.BeginTx(ctx, options)
	if err != nil {
		if db.closed.Load() {
			return ErrClosed
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("begin transaction: %w", ErrTransaction)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			result = errors.Join(result, ErrTransaction)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("commit transaction: %w", ErrTransaction)
	}
	committed = true
	return nil
}

func (db *DB) withMigrationTx(ctx context.Context, fn func(*sql.Tx) error) (result error) {
	if db == nil || db.migrationPool == nil || db.closed.Load() {
		return ErrClosed
	}
	if ctx == nil || fn == nil {
		return ErrInvalidConfig
	}
	tx, err := db.migrationPool.BeginTx(ctx, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if db.closed.Load() {
			return ErrClosed
		}
		return fmt.Errorf("begin migration transaction: %w", ErrTransaction)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			result = errors.Join(result, ErrTransaction)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("commit migration transaction: %w", ErrTransaction)
	}
	committed = true
	return nil
}
