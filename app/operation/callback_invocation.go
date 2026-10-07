package operation

import (
	"context"
	"database/sql"
	"errors"
)

// invokeCallback is a private lifetime boundary, not an operation executor.
// Its owner supplies the transaction and remains responsible for current
// authority, commit/rollback and suppressing output when commit fails.
func invokeCallback(ctx context.Context, definition Definition, invocation InvocationContext, input any, tx *sql.Tx) (CachedResult, error) {
	if ctx == nil {
		return CachedResult{}, errCallbackContext
	}
	if tx == nil {
		return CachedResult{}, errNilSQLTx
	}
	if definition.metadata.OperationID == "" || definition.decode == nil || definition.resolve == nil || definition.encode == nil || definition.validateOutput == nil || definition.invoke == nil || definition.handle == nil || definition.complete == nil {
		return CachedResult{}, ErrInvalidDefinition
	}
	db, invalidate, err := newCallbackDB(tx)
	if err != nil {
		return CachedResult{}, err
	}
	invocation = cloneInvocationContext(invocation)
	invocation.DB = db
	result, err := func() (value any, resultErr error) {
		// This inner boundary finishes drain before result-kind or codec work.
		// During a handler panic the defer runs and the original panic continues.
		defer func() {
			if cleanup := invalidate(); cleanup != nil {
				if resultErr == nil {
					resultErr = cleanup
				} else {
					resultErr = errors.Join(resultErr, cleanup)
				}
			}
		}()
		return definition.handle(ctx, invocation, input)
	}()
	if err != nil {
		return CachedResult{}, err
	}
	return definition.complete(result)
}
