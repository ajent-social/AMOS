# Operation persistence and invocation audit

Status: proposed for independent design review. This is an integrator-owned
implementation of the existing operation-invocation contract, within the same
replacement batch. It grants no runtime dispatch, provider operation or task
acceptance. Existing authorizer, executor, effects and host gates remain open.

## Ownership and sequence

The integrator assigns the previously reserved candidate sequence 17 to
`000017.operation.invocations`. Existing migration IDs and SQL through 16 are
immutable. The assignment covers the new fragment and constructor, its inclusion
in the existing reference/reconcile/billing/generated compositions, invocation
persistence, and the finite invocation audit extension. It does not allocate the
continuity application fragment or alter another owner's identity source.

Source ownership: `app/operation/persistence.go`,
`app/operation/sqlstore/**`, `audit/**`, the new
`migrations/fragments/operation-invocations.sql`,
`migrations/operation_invocations.go`, associated migration tests, and the minimal
existing composition call sites. No executable operation dispatcher is enabled.
Independent exact-head source and privacy review plus real required-service
migration/transaction checks are required before integration.

## Import direction and exact types

The earlier storage example uses unqualified `CachedResult` across packages.
To avoid an import cycle when the executor is implemented, the data and consumer
interface live in `app/operation`, and `app/operation/sqlstore` imports that
package. Its implementation has package name `sqlstore`; there is no competing
result or policy representation.

```go
// app/operation
// Scope is a persistence selector, never authority.
type Scope struct {
    InstallationID, ApplicationID, EnvironmentID, WorkspaceID identity.ID
    ActorKind string
    ActorID identity.ID
    OperationID string
}
type KeyDigest [32]byte
type RequestHash [32]byte
type ReplayContract struct {
    OperationRevision string
    DescriptorDigest, InputSchemaDigest, OutputSchemaDigest [32]byte
}
type Invocation struct {
    ID identity.ID
    Scope Scope
    RequestHash RequestHash
    OperationRevision string
    DescriptorDigest, InputSchemaDigest, OutputSchemaDigest, ResultSHA256 [32]byte
    Result CachedResult
}
type ClaimKind string
const (
    ClaimNew ClaimKind = "new"
    ClaimReplay ClaimKind = "replay"
    ClaimConflict ClaimKind = "conflict"
    ClaimCapacityUnavailable ClaimKind = "capacity_unavailable"
)
type TransactionStore interface {
    WithTx(context.Context, func(*sql.Tx) error) error
    ClaimTx(context.Context, *sql.Tx, identity.ID, Scope, KeyDigest,
        RequestHash, ReplayContract, int) (Invocation, ClaimKind, error)
    CompleteTx(context.Context, *sql.Tx, identity.ID, CachedResult) error
}
// app/operation/sqlstore
func New(*sql.DB) (*Store, error)
```

The store rejects nil context/transaction, non-UUIDv7 identifiers, actor kinds
other than `person`, invalid stable operation IDs/revisions, zero digests and
reservation limits outside 1..65536 before SQL. It returns finite invalid or
unavailable errors without raw database diagnostics. `WithTx` owns a single
READ COMMITTED transaction and propagates callback failure through rollback;
no automatic retry, provider call or independent commit occurs. Caller-owned
transactions must also be READ COMMITTED. The caller retains authority and
final disclosure duties; `ClaimReplay` is never permission to publish.

## Durable rows and capacity

Four new tables use the `amos_operation_` prefix: `installation_capacity`,
`actor_capacity`, `workspace_capacity`, and `invocations`. Installation capacity
is keyed by installation. Actor capacity is keyed by installation, application,
environment, actor kind and actor ID. Workspace capacity adds workspace ID.
Each actor's allocation is positive and strictly below installation hard bytes;
each workspace allocation is positive, at most its actor allocation and strictly
below installation hard bytes. The sum of actor allocations is at most the
installation hard bytes. Usage is nonnegative and at most the corresponding
allocation. All counters are bigint with checked subtraction rather than
unchecked arithmetic. No quota is implicit or auto-created by a claim.

Capacity installation and allocation are owner provisioning, not normal request
work. The first store exposes no quota mutation API or automatic provisioning.
The SQL fragment enforces owner allocation changes with triggers that lock the
installation allocator before actor rows, and actor before workspace rows.
It disallows deletion or reducing an allocation below recorded consumption;
installation hard-limit reductions are outside this slice. Normal claims update
only usage columns and do not lock the installation allocator. Capacity reports
and an authorized owner allocation command remain explicit dispatch prerequisites.
Fixture provisioning is not qualification of that future owner interface.

Invocation rows carry UUIDv7 ID, the complete scope, a 32-byte key digest,
32-byte request hash, frozen descriptor/schema digests, revision, reservation
bytes, finite pending/completed state, finite result kind, canonical JSON bytes
and their SHA-256, and database transaction timestamps. The unique key is the
complete scope plus key digest. No raw request, key, authority snapshot, provider
diagnostic or transport headers are stored. Completed records have no expiry,
eviction, deletion or tombstone operation. Result bytes are bytea, not jsonb, so
canonical bytes survive exactly; length is 1..65536 and bounded by reservation.
A deferred constraint prevents committing an unfinished pending invocation.

`ClaimTx` locks actor then workspace capacity rows, then the exact scoped key
row. Missing/inconsistent capacity fails unavailable. An existing completed row
is examined before attempting any new reservation: exact descriptor/revision/
schema and request hash gives replay; changed binding/input gives conflict;
malformed stored state/digest/kind/size gives unavailable. Neither replay nor
conflict reserves more bytes. A new claim reserves the declared maximum in both
counters and inserts its pending row in the caller transaction. Exhaustion
returns `ClaimCapacityUnavailable` with no claim or counter increment. If an
unexpected unique-key race/SQL failure occurs, return unavailable and require
rollback, never retry inside the transaction.

`CompleteTx` locks the pending invocation and its already-held capacity rows;
it is only valid in the same transaction after `ClaimNew`. Persist the exact
result and digest, release maximum-minus-actual bytes from both counters, and
mark completed. Unknown, already-completed, oversized, invalid-kind or empty
results fail. Store-level validation checks JSON syntax and bounded bytes;
registered output codec validation and replay schema validation remain executor
responsibilities. No checksum is described as authenticated storage.

## Finite audit extension

Add `audit.ActionOperationInvoked`, `audit.ResourceInvocation` and an
`OperationID string` field (`operation_id,omitempty`) on `audit.Event`.
The action/resource pair is exclusive: operation.invoked requires invocation,
a stable operation ID matching the existing operation-ID grammar and length
3..120, a UUIDv7 invocation/attempt resource ID, and zero attributes. Other
actions require empty operation ID and cannot use the new resource type.
Outcomes remain succeeded/denied/unavailable. No conflict outcome is invented.
The additive migration changes only the corresponding audit constraints and
adds the nullable operation_id column; existing rows, append-only functions and
row/truncate triggers remain intact. Existing events serialize without a new
empty field. Read/scan validation includes the operation ID.

Add a separate `audit/sqlstore.InvocationWriter` with pool-free
`NewInvocationWriter() *InvocationWriter` and
`AppendInvocationTx(context.Context, *sql.Tx, identity.ID, string,
 audit.Outcome, identity.ID) error` (invocation/attempt ID, registered operation
ID, outcome, server correlation ID). It derives actor and complete scope only
from the existing trusted principal and workspace selection context, validates
their realm agreement and person actor, and never accepts attribution fields.
This is internal trusted persistence, not a policy evaluator or caller-facing
write permission. The executor must establish current authority before calling
it. Denied attempts may append their finite event without granting the attempted
operation. Ordinary `Store.AppendTx` rejects operation.invoked, so its generic
write-authorizer API cannot substitute for the invocation writer. Both writers
share one private bounded insert helper. No event is committed independently.

## Required evidence

Prove old migration ID/checksum identity, fresh and existing-prefix migration
composition, finite audit validation/privacy and append-only rejection; actual
SQL actor/workspace allocation bounds and lock ordering; exact byte replay,
changed-input/binding conflict, per-realm/key isolation, concurrent same-key
claim, full counter/result rollback, completion shrink, no incomplete commit,
corrupt stored output rejection and missing prerequisites failing visibly.
Audit writer evidence must use actual trusted middleware context; fixtures that
fabricate actor/scope values do not qualify attribution. Callback, authorizer,
effect, codec and transport integration remain required after this storage slice.
