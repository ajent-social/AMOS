package apphost

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	errRuntimeBindingConfiguration = errors.New("runtime binding configuration unavailable")
	errRuntimeBindingUnavailable   = errors.New("runtime binding unavailable")
)

// runtimeBindingReady checks a preprovisioned realm without initializing it.
// The caller owns the runner; this check proves neither schema nor role readiness.
func runtimeBindingReady(ctx context.Context, db storage.TxRunner, installation, application, environment uuid.UUID) error {
	if ctx == nil || nilBindingRunner(db) || !localID(installation) || !localID(application) || !localID(environment) {
		return errRuntimeBindingConfiguration
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	matched, mismatch := false, false
	err := db.WithTx(bounded, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true}, func(tx *sql.Tx) error {
		if tx == nil {
			return errRuntimeBindingUnavailable
		}
		var storedInstallation, storedApplication, storedEnvironment uuid.UUID
		err := tx.QueryRowContext(bounded, `SELECT installation_id, application_id, environment_id FROM public.amos_runtime_binding WHERE singleton = true`).Scan(&storedInstallation, &storedApplication, &storedEnvironment)
		if errors.Is(err, sql.ErrNoRows) {
			mismatch = true
			return errRuntimeBindingConfiguration
		}
		if err != nil {
			return errRuntimeBindingUnavailable
		}
		if storedInstallation != installation || storedApplication != application || storedEnvironment != environment {
			mismatch = true
			return errRuntimeBindingConfiguration
		}
		matched = true
		return nil
	})
	if err == nil && matched {
		return nil
	}
	// Only the unchanged callback error is configuration failure. In particular,
	// a joined rollback failure must not conceal unavailable transaction cleanup.
	if mismatch && err == errRuntimeBindingConfiguration {
		return errRuntimeBindingConfiguration
	}
	if cause := bounded.Err(); cause != nil {
		return errors.Join(errRuntimeBindingUnavailable, cause)
	}
	return errRuntimeBindingUnavailable
}

func nilBindingRunner(db storage.TxRunner) bool {
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
