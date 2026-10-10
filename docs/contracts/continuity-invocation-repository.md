# Continuity repository use inside an operation callback

Status: proposed finite B1 adapter; different exact-head design review is required
before source. The existing operation contract explicitly requires an adapter for
repositories that currently require raw sql.Tx. This proposal supplies that value
adapter only; no operation executor, callback invocation or host is enabled.

## Exact additive API and custody

Add to `examples/continuity/repository`:

```go
func NewWithDBTX(db operation.DBTX, scope Scope) (*Repository, error)
```

Preserve `New(tx *sql.Tx, scope Scope)` and all existing repository methods and
SQL. Internally retain only the canonical operation.DBTX interface plus copied
Scope. The old constructor wraps its exact caller-owned sql.Tx in a private
three-method adapter; it does not create another transaction or pool. Its query
methods adapt sql.Row/sql.Rows to the existing operation.Row/Rows interfaces.
The adapter is not embedded, exported or recoverable through a repository method.
No getter, lifecycle method, credential or arbitrary transaction callback is added.

NewWithDBTX rejects nil and typed-nil values and invalid scope before any method
call or I/O. It retains the supplied interface without reflection into its fields,
concrete type assertion, unwrap call or replacement. A valid Scope remains only
a selector, never authority. No principal/session check is performed by this
constructor and a successful construction does not attest that a supplied DBTX
is guarded or authorized. The later trusted operation handler supplies exactly
InvocationContext.DB and a scope derived from its refreshed selection.

Inside the operation callback, every repository Exec/Query/QueryRow must go
through that exact interface. The repository must not retain or recover raw
sql.Tx on this path. Existing invalidation/cancellation and row lease behavior
therefore remains owned by the operation callback guard; this adapter cannot
extend its lifetime. After invalidation, driver/guard failures map to existing
ErrUnavailable with zero results, never success. Invalid selectors still fail
before I/O as before. Calls are serialized by the caller; no concurrency guarantee
or automatic retry is added.

The legacy New path retains its existing caller-transaction behavior. It does
not become guarded merely by using the same interface internally. Changing that
path's trust or claiming it invalidates at callback return would be incorrect.
All query bytes, parameter ordering, row locks, revision checks, source digests,
human-decision restrictions and atomic activity/draft rules stay unchanged.

## Source ownership and evidence

The assignment owns repository.go, a new invocation_db.go, new scoped tests and
repository README documentation. Existing read/list/write/domain/migration files
are not rewritten. No module, schema, registry, public handler, callback guard or
authority package changes are authorized by this adapter proposal.

Pure tests prove nil/typed-nil and scope rejection without calls; valid construction
performs no I/O; subsequent valid operations forward original context, exact scoped
arguments and SQL to the retained interface; errors and failed row scans return
zero values. Invalid input must not call the interface. Test doubles are confined
to test files and do not qualify a real transaction or callback lifetime.

Required-service checks separately exercise the new constructor with a real
caller transaction and the legacy constructor, proving ordinary retrieval,
revision/activity/draft behavior and caller rollback still use the same rows.
The test-only transaction adapter has the exact same three methods and no fake
storage; it is not advertised as a production callback guard. Runtime fixture
source/custody must be independently qualified before those tests run. Missing
required service is a visible failure, never a skip.

Actual operation callback lifetime qualification remains an integration gate:
the complete executor must pass its real guarded DBTX into the repository, then
prove retained repository/row calls fail after callback completion and cannot
write or disclose through an escaped transaction. The adapter cannot obtain
that evidence from a fake DBTX or an ordinary transaction shim. Existing callback
guard evidence is reused at its exact boundary, not relabeled as this integration.

Normal/race/vet/pinned lint, meaningful negative/restored checks and separate
exact-head source review apply. Complete operation policy, resource locking,
capacity/replay/audit/effects, transaction completion, HTTP mutations, RB1-RB10
and full provider/product/release gates remain required. This change removes a
concrete type incompatibility without authorizing mutation dispatch or accepting
T2.8, WEB, HOST or the replacement mission.
