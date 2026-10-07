# Transaction-only durable job store (v1.17)

Status: proposed; freezes after independent exact-head review and merge.

Add a transaction-only job-store constructor `NewWithTx(TxRunner, Config) (*Store, error)`. Its consumer-owned `TxRunner` has exactly `WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error`. Preserve `New(*sql.DB, Config)`, `NewTxWriter(Config)` and caller-owned `EnqueueTx` signatures. The runtime constructor stores no raw pool or DSN and opens no connection. It rejects nil and typed-nil runners and snapshots configuration identically to the legacy constructor. A runner is trusted infrastructure: it must invoke the callback once synchronously in one context-bound transaction, commit only after callback success, roll back failure, preserve panic identity after attempted rollback, honor transaction options and never replay callbacks.
>
Standalone Enqueue, Get, Claim and Resolve each own exactly one runner transaction. Runtime operations explicitly select READ COMMITTED; Get additionally requests read-only. Results are provisional inside the callback and are returned only after successful commit; transaction failure returns a zero Job and, for Claim, false. Empty Claim commits scoped maintenance before returning false. Store code never commits, rolls back, retains or escapes the callback transaction. Existing EnqueueTx and TxWriter continue to use the caller's transaction without starting or finishing another one; callers acknowledge durable intent only after their own commit.
>
Preserve database-clock scheduling, scoped claim maintenance, separate execution/reconciliation budgets, monotonic fences and external-outcome reconciliation. Resolve acquires the job row lock before a separate final database-clock lease check and guarded update, so time spent waiting for a lock cannot authorize an expired worker. A missing row returns ErrNotFound; a present row with failed fencing returns ErrLeaseLost. Provider calls run after Claim commits and outside every store transaction. No automatic transaction retry, remote replay, lease renewal, migration, pool exposure or new connection configuration is introduced.
>
Context cancellation binds pool admission, SQL execution, lock waits and transaction completion. Valid-operation cancellation remains detectable with errors.Is for the context sentinel and the store unavailable sentinel; other infrastructure/driver errors map to ErrUnavailable without raw diagnostics. Preserve domain validation/conflict/not-found/lease-lost sentinels. Additional rollback failure cannot be reported as a clean domain rejection: return a safe error joining the domain sentinel and ErrUnavailable. Nil-context/invalid-input behavior retains existing method-specific errors. A failed or uncertain commit never returns a successful Job or claim. This does not guarantee a transaction cannot have committed before a cancellation or connection failure becomes observable.
>
Required local PostgreSQL tests prove runtime-handle composition, atomicity, commit-error result suppression, cancellation and lock-wait fencing with a precreated jobs schema and DML-only role. Missing prerequisites fail explicitly; no mock, local role or source acceptance qualifies a provider or production deployment.

## Compatibility and ownership

Consumer-owned TxRunner is structurally compatible with storage.TxRunner. Keep
legacy New signature, Config fields and TxWriter behavior. The private legacy
pool runner uses the supplied pool and does not close it; preserve its default
isolation/options policy. Only NewWithTx pins READ COMMITTED, with read-only Get.
No stored data or migration change is required. Get and Resolve may gain explicit
transaction overhead. Do not call standalone methods inside a held caller
transaction on a one-connection pool; use EnqueueTx for atomic intent.

Author owns store.go, new txrunner.go and their package tests only. Coordinator
owns shared contracts and later production composition. Runtime qualification uses
an independently precreated unchanged jobs schema, exact-owned test rows, DML-only
role and no test-time DDL. Test post-lock expiry with two real connections, unknown
outcome reconciliation, scope filters on maintenance and claims, concurrency,
idempotency, joint domain rollback, zero results on transaction failure and absent
service failure. Separate review and fresh landed service checks are required.
No provider call occurs in a store transaction; this component grants no executor
or production authority.
