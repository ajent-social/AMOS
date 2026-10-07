package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
)

// TxRunner runs the callback synchronously exactly once in a context-bound
// transaction, honors options, commits success and rolls back failure. It must
// preserve callback panic identity after rollback and must never replay work.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

// NewWithTx constructs a store without opening or taking ownership of a pool.
// Standalone operations use READ COMMITTED; Get also requests read-only access.
func NewWithTx(runner TxRunner, cfg Config) (*Store, error) {
	if runner == nil {
		return nil, ErrInvalidConfig
	}
	value := reflect.ValueOf(runner)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, ErrInvalidConfig
		}
	}
	return newStore(runner, cfg, true)
}

func newStore(runner TxRunner, cfg Config, runtimeOptions bool) (*Store, error) {
	if !validConfig(cfg) {
		return nil, ErrInvalidConfig
	}
	cfg.ClaimKinds = append([]string(nil), cfg.ClaimKinds...)
	cfg.ClaimScopes = append([]ClaimScope(nil), cfg.ClaimScopes...)
	if cfg.ClaimScopes == nil {
		cfg.ClaimScopes = []ClaimScope{}
	}
	scopes, err := json.Marshal(cfg.ClaimScopes)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return &Store{runner: runner, runtimeOptions: runtimeOptions, config: cfg, claimScopesJSON: string(scopes)}, nil
}

func (s *Store) withTx(ctx context.Context, readOnly bool, fn func(*sql.Tx) error) error {
	var options *sql.TxOptions
	if s.runtimeOptions {
		options = &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly}
	}
	var callbackErr error
	err := s.runner.WithTx(ctx, options, func(tx *sql.Tx) error {
		callbackErr = fn(tx)
		return callbackErr
	})
	if err == nil {
		return nil
	}
	// Our callbacks return only safe, comparable sentinels. A runner must return
	// that same error on clean rollback; an added failure is infrastructure loss.
	var safe error
	if callbackErr != nil {
		safe = callbackErr
	}
	if callbackErr == nil || err != callbackErr {
		safe = errors.Join(safe, ErrUnavailable)
	}
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, sentinel) {
			safe = errors.Join(safe, ErrUnavailable, sentinel)
		}
	}
	if ctx.Err() != nil {
		safe = errors.Join(safe, ErrUnavailable, ctx.Err())
	}
	return safe
}

// poolRunner preserves legacy default options and never closes its supplied pool.
type poolRunner struct{ db *sql.DB }

func (r poolRunner) WithTx(ctx context.Context, options *sql.TxOptions, fn func(*sql.Tx) error) (result error) {
	tx, err := r.db.BeginTx(ctx, options)
	if err != nil {
		return ErrUnavailable
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, ErrUnavailable)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}
	finished = true
	return nil
}
