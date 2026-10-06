# Shared operation invocation contract (proposed v1.12)

Status: proposed under ADR 023; independent review is pending. This document does not yet amend frozen v1.11, assign a migration sequence, or authorize runtime dispatch. The integrator owns adoption, task-scope reconciliation and shared API changes.

## Decision and ownership

All web, REST, MCP, and proxy adapters use one `app/operation` executor. It consumes only the verified principal and current workspace selection attached by trusted middleware; caller selectors and resource IDs are untrusted input until the executor resolves and rechecks them. It returns one logical result/error that each transport maps using its existing contract. No transport maintains a policy table or calls a domain mutation outside the executor.

The canonical exported policy surface remains the frozen root `policy/` contract in `docs/planning/contracts.md`, owned by the root integrator. `app/operation` consumes `policy.Evaluator`; it does not add transport-specific policy types or define a competing evaluator. Before dispatch, the root must reconcile the existing T2.8 `app/policy/` scope with this ownership and publish an explicit integrator-owned implementation/adapter assignment. A worker may not create or edit `policy/`, `audit/`, migrations, migration registry, app composition, or executable wiring under the current source-task scope.

The operation executor always rechecks current authority before new work and before replay disclosure. It does not persist or reuse an `Allowed` decision. `Allowed`, `Denied`, and `Unavailable` remain distinct. Missing/revoked authority or insufficient permission is denied; inability to establish current state is unavailable; neither result is converted to the other.

For a required-idempotency write, one database transaction owns the domain mutation, any durable external-effect intent, the replay result, and the success audit event. Providers are never called inside this transaction. External work is enqueued using the existing durable intent/reconciliation boundary; remote exactly-once execution is never claimed. Same realm/key and same canonical input returns the original logical result; same realm/key with changed input conflicts. Retain replay-safe results for the complete lifetime of the idempotency record. There is no response expiry, tombstone transition, or cleanup behavior in this proposal. Any later deletion/retention rule requires a separate owner decision, privacy review, and contract amendment.

## Frozen policy contract

Keep the names and semantics already frozen in `docs/planning/contracts.md`; this draft does not alter them:

```go
package policy

type Decision interface{ isDecision() }
type Resource struct { Type string; ID identity.ID; WorkspaceID identity.ID }
type Allowed struct { PolicyRevision string; EvaluatedAt time.Time; ExpiresAt time.Time }
type Denied struct { Code string; PolicyRevision string; EvaluatedAt time.Time }
type Unavailable struct { Code string; Retryable bool; RetryAfter time.Duration }
type MCPExposure string // never | eligible | challenge
type Requirements struct {
    Permissions []string
    Entitlements []string
    Assurance identity.AssuranceLevel
    MCPExposure MCPExposure
}
type Evaluator interface {
    Evaluate(context.Context, identity.Principal, Resource, Requirements) Decision
}
```

The root integrator supplies the evaluator implementation and a transaction-aware current-state adapter without changing this public method or adding a new permission vocabulary. The adapter resolves current database facts in the invocation transaction and supplies those facts to the frozen evaluator; it must not trust workspace selection, cached grants, or operation metadata as authority.

## Concrete `app/operation` boundary

The executor accepts the whole transport-neutral input needed to identify and validate an operation. Path/query parameters are represented explicitly so the resource resolver and canonical request hash do not silently omit route IDs. Adapters cannot provide a principal, actor, workspace, grant, or assurance value.

```go
package operation

type SideEffect string // read | write | external
type Idempotency string // required | natural | forbidden
type OutputClass string // replay_safe | sensitive | secret

type NameValues struct {
    Name string
    Values []string
}
type RawInput struct {
    Path []NameValues
    Query []NameValues
    Body []byte
}
type Request struct {
    OperationID string
    IdempotencyKey string // empty only when the registered operation permits it
    Input RawInput
}

type Metadata struct {
    OperationID string
    Method string // canonical business route method
    Path string // canonical business route path
    InputType string
    OutputType string
    Features []string // declared extension features; copied and validated
    Revision string // immutable revision for this complete operation contract
    InputSchemaDigest [32]byte
    OutputSchemaDigest [32]byte
    Requirements policy.Requirements
    SideEffect SideEffect
    Idempotency Idempotency
    OutputClass OutputClass
    MaxReplayBytes int // required-idempotency writes: 1..65536
}

// Input decoding validates every path/query/body field against the registered
// schema, rejects unknown/duplicate fields and duplicate JSON object keys,
// decodes to I, and deterministically serializes that typed value. It never
// hashes arbitrary raw JSON or an unvalidated map.
type InputCodec[I any] interface {
    DecodeCanonical(RawInput) (typed I, canonical []byte, err error)
}

// Resource resolution maps validated input to the authorization resource and
// complete affected-resource lock set. WorkspaceID always comes from trusted
// Selection, never the request; the authorizer locks and checks tenancy.
type ResourceResolver[I any] interface {
    Resolve(identity.Principal, workspacecontext.Selection, I) (ResolvedResources, error)
}
type ResolvedResources struct {
    AuthorizationResource policy.Resource
    LockSet []policy.Resource // complete bounded set, unique and sorted by UUID
}

type ResultKind string // succeeded | created | accepted | no_content
type Result[O any] struct {
    Kind ResultKind
    Value O
}
type CachedResult struct {
    Kind ResultKind
    CanonicalJSON []byte
}

// OutputCodec serializes typed output to canonical JSON, validates it against
// the registered output schema, and validates stored bytes again on read.
type OutputCodec[O any] interface {
    EncodeCanonical(O) ([]byte, error)
    ValidateCanonical([]byte) error
}

// DBTX deliberately omits Begin/Commit/Rollback. The callback receives a
// private transaction wrapper, not *sql.Tx or an embedded transaction.
type DBTX interface {
    ExecContext(context.Context, string, ...any) (sql.Result, error)
    QueryContext(context.Context, string, ...any) (Rows, error)
    QueryRowContext(context.Context, string, ...any) Row
}
type Rows interface {
    Next() bool
    Scan(...any) error
    Err() error
    Close() error
}
type Row interface { Scan(...any) error }

type InvocationContext struct {
    Principal identity.Principal
    Selection workspacecontext.Selection
    Resource policy.Resource
    LockedResources []policy.Resource // immutable copy, sorted by UUID
    InvocationID identity.ID
    DB DBTX // private wrapper; no transaction lifecycle methods
    Effects EffectEnqueuer // already bound to this invocation's private tx
}

// Definition is opaque to business packages: all fields and erased closures
// are private. Bind preserves the accepted typed-handler pattern while adding
// a transaction-scoped invocation context in place of the earlier proposed
// Principal parameter.
type Definition struct {
    metadata Metadata
    decode func(RawInput) (any, []byte, error)
    resolve func(identity.Principal, workspacecontext.Selection, any) (ResolvedResources, error)
    encode func(any) ([]byte, error)
    validateOutput func([]byte) error
    invoke func(context.Context, InvocationContext, any) (CachedResult, error)
}
type Registry interface { Lookup(string) (Definition, bool) }

// Bind is the only definition constructor. Business handlers stay typed; the
// private closure is the only place input/output types are erased.
func Bind[I, O any] (Metadata, InputCodec[I], ResourceResolver[I],
    OutputCodec[O], func(context.Context, InvocationContext, I) (Result[O], error)) (Definition, error)

type InvocationAuditWriter interface {
    // Derives actor and tenant scope only from the trusted context. This
    // internal writer can record a denied attempt but cannot grant the
    // attempted operation or accept caller-supplied attribution.
    AppendInvocationTx(context.Context, *sql.Tx, identity.ID,
        string, AuditOutcome, identity.ID) error // invocation ID, operation ID, outcome, correlation ID
}
type AuditOutcome string // succeeded | denied | unavailable

type EffectEnqueuer interface {
    // The adapter overwrites InstallationID/ApplicationID with the trusted
    // invocation scope, derives Intent.Key from invocationID and a unique
    // effect ordinal, and validates the registered kind, strict typed payload
    // codec/schema, byte limit, privacy class, external-effect flag, and
    // deadline bounds before delegating to jobs.EnqueueTx. Reusing an ordinal
    // within one invocation is rejected, including for a different payload.
    // Handler input cannot select another tenant's job scope. The client key
    // is never reused as the jobs key. This is tx-bound and accepts no tx arg.
    Enqueue(context.Context, uint32, jobs.Intent) (jobs.Job, error)
}

type TransactionAuthorizer interface {
    // RecheckCurrentTx reads and locks authoritative state in this tx,
    // compares the trusted middleware principal/selection to current rows,
    // reads database time itself after acquiring required row locks, and then
    // evaluates the frozen policy contract. Returned decision timestamps are
    // database-derived; no caller-supplied time is accepted.
    RecheckCurrentTx(context.Context, *sql.Tx, identity.Principal,
        workspacecontext.Selection, policy.Resource, []policy.Resource,
        policy.Requirements) policy.Decision
}

type Executor interface { Invoke(context.Context, Request) (CachedResult, error) }
```

`Metadata` is the complete runtime binding of the accepted business descriptor:
method, canonical path, typed input/output names, and declared features must
match the descriptor exactly. Registry construction computes and freezes a
canonical descriptor digest over those fields, the operation ID/revision, both
schema digests, and all policy/side-effect/idempotency/MCP metadata. Generated
web/REST routes and MCP tool declarations bind to that frozen entry by
`OperationID`; they cannot supply a different route, method, exposure, or
requirements at request time. Startup rejects missing, duplicate, or mismatched
descriptor and transport bindings. The caller never chooses arbitrary route
metadata to select a handler.

This deliberately amends the earlier proposed handler shape
`func(context.Context, identity.Principal, I) (O, error)`: the handler now
receives `InvocationContext` in place of the separate principal argument and
reads `InvocationContext.Principal`, selection, resource, invocation ID, and
transaction-safe capabilities from it. It preserves the accepted generic
`Bind[I,O]` typed input/output pattern and never exposes `any` to business
handlers. The current business-extension contract describes a design seam;
there is no implemented or published runtime handler API to break. Root must
record this amendment and update the accepted extension seam before dispatch.

The executor privately owns the real `*sql.Tx` and gives the callback only a
non-embedded wrapper implementing `DBTX`; there is no raw-transaction getter,
`Commit`, `Rollback`, `Begin`, or transaction lifecycle method on the callback
surface. The wrapper rejects direct transaction-control statements and is invalidated
when the callback returns; retained `DBTX`, `Row`, or `Rows` values fail after
invalidation. Root integration retains the actual transaction for
`TransactionAuthorizer`, invocation-store completion, and audit append. The
effect enqueuer is already bound to that transaction and takes no transaction
argument. A pre-existing domain participant that requires `*sql.Tx` cannot be
passed through this wrapper as if compatible; root must provide an explicit
transaction-bound adapter or assign a separate integration change before that
participant is used. Handlers are trusted, owner-reviewed application code; this narrow interface prevents accidental transaction ownership changes, not malicious SQL execution. SQL access is not a sandbox and cannot prevent arbitrary database functions or a handler using other process capabilities. The concrete rejection mechanism and its limits require tests before the interface is qualified.

At registration, copy all metadata slices and reject duplicate IDs, missing or invalid requirements, absent codecs/resolver/handler, unsupported side-effect/idempotency combinations, schema-digest mismatch, and mutable aliases. `required` is valid only for write or external-intent operations whose output class is explicitly `replay_safe`, with a strict output codec and maximum result reservation in `1..65536`; `sensitive` and `secret` outputs cannot be replay-cached. Output classification is an explicit reviewable schema decision, not a field-name heuristic. The registry rejects any operation with an undeclared resource resolver. It snapshots before serving and accepts no later registrations.

The resolver receives only schema-validated typed input and trusted `Selection`. It returns the authorization resource plus the complete bounded `LockSet` of affected resources derived from validated route/body/query values, with `WorkspaceID` bound to `Selection.Workspace`; the authorization resource must be in the set. An input workspace selector cannot replace that value; a mismatching selector is rejected. The current-state authorizer checks tenant ownership and locks every resource in the set before capacity/idempotency rows are claimed. For operations whose resource is the workspace itself, the resolver includes the selected workspace. No transport can omit a path parameter that participates in resource resolution or request hashing. A handler may mutate only its declared lock set and rows newly created beneath those locked parents. If an operation cannot resolve a complete set before dispatch, it requires a separately reviewed transaction adapter and cannot use this executor.

The idempotency request hash is `SHA-256("amos-operation-replay-v1" || 0x00 || descriptorDigest || 0x00 || canonicalInput)`. The canonical descriptor digest binds the immutable operation revision, both schema digests, and all remaining descriptor fields; canonical input covers path parameters, query parameters, and body. The durable key scope is server-derived from principal installation/application/environment, actor kind/ID, current selection workspace, and registered operation ID; the client key is digested and never persisted. Enforce a bounded key length and reject empty keys for `required`; the key is never treated as authority. Operation ID remains a separate key-scope column. A same-key request under a different descriptor digest is a conflict and never reruns the callback. Persist the frozen descriptor digest, operation revision, and both schema digests with each completed replay row; on replay, verify them against the registered entry and validate the stored output bytes again. A mismatch fails closed as conflict for a changed registered descriptor or unavailable for corrupt stored data; it never silently reinterprets an old output. Revision changes do not create a new key scope, so they cannot repeat a completed mutation.

`CachedResult` is transport-neutral. It stores a finite `ResultKind` and validated canonical JSON only; it contains no HTTP status, arbitrary headers, cookies, diagnostics, or raw provider data. REST/HTTP adapters map the finite result kind to their status, MCP maps it to its result envelope, and all adapters return the same logical output. Error results use the existing stable error contract and are not serialized as arbitrary operation output. Persist the SHA-256 digest of the canonical result bytes separately with the row. A cached value must pass the result-digest check, stored descriptor/version checks, and output-schema validation when loaded; corrupt or oversized rows fail unavailable and never trigger a fresh mutation.

## Current authority and transaction ordering

The executor obtains `identity.PrincipalFromContext(ctx)` and `workspace/context.FromContext(ctx)` only from trusted middleware. Both are required. Existing identity and ADR 021 contracts keep workspace selection separate from the principal. The root-owned `TransactionAuthorizer` must, inside the active SQL transaction:

- confirm the principal still identifies an active person/account in the current installation/application/environment and compare its security/revocation epoch with current state;
- compare the middleware selection with current active workspace state and ownership/membership, membership epoch, and operation permission rows;
- revalidate the opaque session reference attached by trusted authentication middleware against the current session/revocation state. If the current identity package does not expose a trusted transaction-time session recheck, the root must add that internal seam before dispatch; the executor must never accept a raw session ID/token from the request or handler;
- verify the resolved resource exists in the selected workspace and that current resource tenancy is compatible;
- check assurance against current DB time and revalidate current entitlement state/projection, treating stale positive billing projections (older than the billing contract's five-minute bound or crossing their financial boundary) as `Unavailable`;
- serialize revocation/permission changes against the operation by locking the relevant rows in this order: active person/account, authenticated session, selected workspace, membership and role-version/permission rows, billing account and current projection, then domain resource rows in ascending UUID order. Acquire multiple rows of one class in ascending UUID order. Normal invocations then lock actor and workspace capacity rows, the idempotency row, durable-intent/job rows, and append audit last. Owner quota allocation or increase locks the installation capacity-allocation row before actor and workspace quota rows; normal invocations never lock that installation allocator. This preserves the existing workspace-store requirement to lock person before workspace and keeps multi-resource locks in ascending ID order. Every integrated writer that touches these classes in one transaction must use a compatible order or fail closed as an incompatible adapter before dispatch. If a predicate has no lockable row, use a tested serializable strategy for that predicate. Reconcile this order with each existing store before use; a snapshot copied into middleware context is never sufficient. Deadlock/serialization aborts roll back the whole invocation and return unavailable unless an explicitly bounded whole-transaction retry proves no local or external effect committed.

The transaction authorizer passes current facts to the frozen `policy.Evaluator`; operation metadata supplies requirements only. It does not manufacture a principal, selection, grant, or policy allow. Read failures become `Unavailable`. Missing/revoked membership, account, or selected resource is `Denied` (or the existing non-enumerating resource response); unavailable is never downgraded.

Invocation timestamps and freshness checks use database time only. Persisted `created_at`/`completed_at` use PostgreSQL `transaction_timestamp()` in the transaction. Current-expiry checks read `clock_timestamp()` from the same database after authority locks are held and after any duplicate-row lock wait; no caller `time.Time` or application wall clock controls expiry, assurance, capacity, or ordering. If an `Allowed.ExpiresAt` is passed before commit, the executor rolls back the domain mutation and result.

For each request, the executor generates a fresh internal UUIDv7 attempt ID. It is not caller-controlled. For a required-idempotency write, a new durable invocation uses that ID; an exact replay uses the existing row's ID. The transaction order is:

1. Decode the full input, resolve the resource and derive the server-side scope; reject malformed/unregistered requests before opening a transaction.
2. Begin one transaction and recheck current authority with row/predicate serialization. If denied or unavailable, call `InvocationAuditWriter.AppendInvocationTx` using the attempt ID, registered operation ID, finite outcome, and server correlation ID; commit only that audit event and return the decision. An audit write failure rolls back and surfaces `Unavailable`.
3. Claim/reserve the realm+key row using the attempt ID. A changed request hash returns conflict with no callback and no invocation audit event; it is not a policy denial. An exact duplicate may wait on the existing row; after that wait, re-read database time and recheck current authority before disclosing the stored result. A now-denied or unavailable caller never sees cached output. Capacity exhaustion returns `Unavailable` before callback execution and may write only an unavailable attempt event. Audit failure rolls back and surfaces `Unavailable`.
4. Invoke the registered callback with `InvocationContext` containing the immutable invocation ID, same transaction, principal, selection, complete locked-resource set, and transaction-only effect enqueuer. Domain writes may touch only the prelocked set and new rows beneath those locked parents; any other adapter is incompatible and cannot dispatch. Domain writes and any outbox intent use this transaction. No provider call is permitted.
5. Canonically encode and validate output; enforce the reserved byte maximum; persist the output and release unused capacity; call `AppendInvocationTx` with the invocation ID and succeeded outcome; commit. Any callback, output, effect-intent, audit, or commit error rolls back all mutation/result/audit/effect rows. Retry after rollback uses the same client key and may safely run because no completed local effect or external intent committed.

For policy-denied/unavailable attempts, no idempotency claim, callback, domain mutation, or effect intent is created. Append one audit-only `operation.invoked` event in the same transaction with the fresh attempt ID and finite outcome; commit the event before returning the decision. If the authorizer has made the transaction unusable, or the event cannot be committed, return `Unavailable` and do not claim that the event was recorded. Changed-hash conflicts emit no invocation audit event, because the finite audit vocabulary has no conflict outcome and relabeling a key conflict as policy denied would be false. Cache-capacity exhaustion returns unavailable and may record only a bounded unavailable attempt. An exact authorized replay emits no second success event. A transient callback/output/commit failure rolls back the invocation transaction and is not recorded as a completed success.

## Store and transactional seams

The invocation store, audit extension, and migration are root-integrator-owned preconditions, outside the current T2.8 source-worker paths. They are not an extension of `jobs/sqlstore`.

```go
package operationsqlstore

type Scope struct {
    InstallationID, ApplicationID, EnvironmentID, WorkspaceID identity.ID
    ActorKind identity.ActorKind
    ActorID identity.ID
    OperationID string
}
type KeyDigest [32]byte
type RequestHash [32]byte
type ReplayContract struct {
    OperationRevision string
    DescriptorDigest [32]byte
    InputSchemaDigest [32]byte
    OutputSchemaDigest [32]byte
}
type Invocation struct {
    ID identity.ID
    Scope Scope
    RequestHash RequestHash
    OperationRevision string
    DescriptorDigest [32]byte
    InputSchemaDigest [32]byte
    OutputSchemaDigest [32]byte
    ResultSHA256 [32]byte
    Result CachedResult
}
type ClaimKind string // new | replay | conflict | capacity_unavailable
type TransactionStore interface {
    WithTx(context.Context, func(*sql.Tx) error) error
    ClaimTx(context.Context, *sql.Tx, identity.ID, Scope, KeyDigest,
        RequestHash, ReplayContract, int) (Invocation, ClaimKind, error)
    CompleteTx(context.Context, *sql.Tx, identity.ID, CachedResult) error
}
```

`ClaimTx` uses the executor-generated attempt ID for a new row and atomically reserves the operation's declared maximum result bytes against owner-assigned per-actor and per-workspace quotas. The sum of all actor quota allocations is bounded by the per-installation hard capacity; each workspace allocation is bounded by its actor allocation, and each actor/workspace quota is strictly below the installation capacity. Quotas are explicit capacity allocations, so unassigned bytes cannot be consumed by an unbounded new scope. Normal invocations update only the applicable actor/workspace reservation counters; the installation allocation row is locked only when an owner assigns or increases a quota. All changed quota rows are locked in the stated order before the unique full-realm+key row. A duplicate replay or changed-hash conflict consumes no new reservation. Same hash waits then replays; changed hash conflicts. A new claim reserves its maximum before callback; if reservation or callback fails, transaction rollback releases the reservation and claim. `CompleteTx` stores validated canonical result bytes and their SHA-256 digest, the immutable descriptor digest, operation revision, and both schema digests, then shrinks the charged reservation to actual size. Registration rejects a replay maximum that exceeds any applicable quota; inconsistent runtime capacity configuration or exhaustion fails closed before callback. Capacity use, rejected admissions, configured limits, and remaining bytes must be visible to the installation operator through bounded health/operations reporting. Exhaustion remains unavailable until the operator explicitly increases and assigns capacity. There is no automatic deletion, expiry, eviction, quota reclamation, or tombstone transition: completed replay records persist for their full idempotency-record lifetime, and reallocation of consumed capacity remains a separate owner decision. DB time is read from the active transaction.

The callback receives its immutable invocation ID in the same transaction context. The transaction-bound `EffectEnqueuer.Enqueue` overwrites or validates tenant scope against the trusted invocation principal, derives its durable key from that ID and effect ordinal, and validates the operation-specific kind, strict typed payload schema, byte limit, privacy class, external-effect flag, and deadline bounds before persisting a durable intent; it has no provider transport. An invocation may use each ordinal only once. `InvocationAuditWriter.AppendInvocationTx` receives that ID, operation ID, finite outcome, and correlation ID; it derives actor and scope from trusted context. For policy denials before a durable claim, it receives the internal attempt ID instead. The transaction adapter owns commit/rollback; no in-memory mutex or independently committed audit/outbox call may stand in for SQL atomicity.

## Audit extension

The current audit contract is finite and append-only. Its integrator-owned schema and Go API do not yet accept an `operation.invoked` action, an `invocation` resource, or `operation_id`. Root must extend the Go event validator/append/read API and the SQL constraints in the same additive sequence as the invocation table. The event has exactly one action (`operation.invoked`), one resource type (`invocation`), a UUIDv7 resource ID, existing finite outcome (`succeeded`, `denied`, `unavailable`), server-resolved tenant and actor fields, correlation ID, and a separately validated stable `operation_id` required only for this action. Do not put request/key digest, input/output, permission lists, session data, or free-form error/provider detail in attributes. Exact replay does not append another success event. Audit rows remain append-only and under the existing retention rule.

The root-owned transaction audit adapter is privileged internal persistence, not a caller-facing audit write permission: policy denial must still be able to append its bounded denial event using trusted middleware attribution, without authorizing the denied operation. It must not accept actor/scope/operation ID from request payload. Audit failures follow the transaction rules above: successful mutations roll back if success audit cannot commit; denial/unavailability returns unavailable if its audit-only event cannot commit. Independent privacy/security review is required before landing the schema/event extension.

## Migration and composition

Current migration IDs and SQL bytes through 16 are immutable. Sequence 17, proposed ID `000017.operation.invocations`, is only a root-assigned candidate; no worker may allocate it, edit the registry, alter migrations 1–16, or claim runtime availability. Root must assign migration content, the audit API/schema change, the invocation store, and every supported migration composition as integrator-owned work before enabling `app/operation`.

The additive migration creates the scoped invocation table, installation capacity allocation, and actor/workspace quota and usage rows; allocated quota totals can never exceed the installation hard limit. Normal claims update only their actor/workspace usage rows; owner assignment/increase performs the installation-level allocation transaction. It uses a unique constraint on `(installation_id, application_id, environment_id, actor_kind, actor_id, workspace_id, operation_id, key_digest)`, UUIDv7 IDs, bounded result/class/size constraints, persisted descriptor/replay/result digests, explicit JSON/content classification, and indexes needed for scope lookups. It never stores raw client keys, raw request/input, or diagnostic text. The same migration extends audit checks for the finite action/resource/operation ID and preserves existing append-only triggers. All DDL is transactional; exact old checksums and IDs are verified unchanged. Every supported composition must include sequence 17 before the executor can be registered. Migration/registry and executable wiring remain root-owned.

## Prerequisites and ownership

- Root reconciles the T2.8 task path `app/policy/` with frozen integrator-owned `policy/`; root supplies the frozen evaluator and transaction-authorizer implementation. No new permission vocabulary or transport wire permission is introduced.
- T2.8 worker scope is only `app/operation/**`; root separately owns invocation SQL storage, `audit` API/schema, sequence 17, migration registration, and executable composition. These dependencies must land or be explicitly co-developed by the integrator before runtime use.
- Root reconciles the operation callback with the accepted typed business-extension contract and generator seam; this proposal requires full path/query/body decoding, `ResourceResolver`, `OutputCodec`, and invocation ID in the shared transaction context.
- Independent security/privacy review approves the finite audit extension and replay-output classification. Owner separately approves any future retention/capacity reclamation rule; no replay expiry is implied here.

## Verification plan

Unit tests cover duplicate/invalid registry metadata, complete descriptor and transport-binding mismatch, unknown/duplicate JSON fields, typed canonicalization, path/query/body and complete lock-set resolution, workspace mismatch, output validation/classification/limits, tenant-bound effect enqueueing, duplicate ordinal rejection, and rejection of unknown, oversized, or privacy-disallowed effect payloads. They also cover stable result mapping and the three policy outcomes. Same-key operation-revision/output-schema changes conflict without a second callback; corrupt result digest/bytes fail unavailable. Missing principal or current selection cannot invoke a callback; stale/revoked current state returns the frozen denied/unavailable result without disclosing replay data.

Real PostgreSQL tests cover same realm/key/hash returning exact canonical result; changed hash/revision conflict with no callback or false denial audit; actor/workspace/environment/operation isolation; concurrent duplicate callback exactly once; active account/session/security-epoch, membership, resource, assurance and entitlement rechecks; lock ordering against revocation and membership mutation; database-time behavior after lock waits; callback/output/audit/outbox/capacity failure rollback; denied/unavailable audit-only commit; replay audit behavior; installation, actor, and workspace capacity reservation/fairness before callback; operator capacity increase; and migration 17 after unchanged migrations 1–16. Audit tests reject arbitrary action/resource/operation ID and payloads and preserve append-only enforcement.

Transport integration proves web/REST/MCP/proxy map one executor's logical result/error and cannot bypass current authorization. These checks establish local source behavior only; no provider, deployment, production-readiness, hosted-CI, or full task-acceptance claim follows from this proposal.
