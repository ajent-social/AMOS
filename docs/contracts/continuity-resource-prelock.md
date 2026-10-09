# Continuity mutation resource prelock

Status: finite early design for different review before the separately assigned
repository source slice. Baseline `ecab2421c77ae2ad6eee56f46e67903267e6f67a`.
This removes a concrete repository ordering dependency; it grants no operation,
principal, workspace, permission, entitlement or transaction authority. The
[operation contract](operation-invocation.md), [repository callback adapter](continuity-invocation-repository.md)
and complete native writer/F/commit gates remain mandatory.

## API and caller obligation

Add only this repository method and observation type:

```go
type LockedResource struct {
    Kind string
    ID string
}
func (r *Repository) PrelockMutation(ctx context.Context, action, id string) ([]LockedResource, error)
```

`action` accepts exactly the existing activity names `case.changed`,
`checklist.changed`, `procedure.changed`, `draft.saved`; `id` is the corresponding
existing case/application/procedure UUIDv7. They select a finite native algorithm,
not a caller lock list or permission. Invalid/unsupported selectors fail
ErrInvalid before SQL. Nil/canceled context or unusable dependency fails closed
with no returned list. No callback, SQL text, table name, clock or supplied facts
are accepted. The repository's existing copied Scope still selects all four realm
columns and never attests authority.

The owning native invocation calls this method once, after its current identity,
workspace/permission and billing locks, before capacity/idempotency rows and the
callback, using the same active writable READ COMMITTED transaction. The method
checks actual isolation/read-only state through its retained DBTX. It never
begins, commits, rolls back, unwraps or replaces that capability. A closed or
invalidated interface fails unavailable. The caller serializes use and rolls
back on **every** error, including a partial-acquisition failure.

The returned list is a copied observation of this successful prelock, not a
transferable capability or a substitute for the executor's complete lock set.
It contains no transaction handle, principal or constructor/setter accepting
asserted locks. Its lifetime is only the original transaction; retaining or
mutating it grants nothing in another transaction. The primitive cannot attest
an arbitrary DBTX's origin or prevent a caller using a second repository. The
native composition must prove one prelock for one declared mutation and no prior
domain locks, additional mutations or newly discovered resources. Multiple
prelock calls, mixed-operation callbacks and retained-handle authority remain
unsupported until a separately reviewed aggregate plan exists.

## Discovery, closure and global acquisition order

Current `Case(ctx,id)` calls `caseRecord(...,false)`, which still calls
`references` and takes source SHARE locks. It must **not** be used for discovery.
Factor existing private row-read/decoding helpers in `read.go` so case and
application discovery can read/decode just their own row with no relation lookup
or lock clause. The legacy public reads and writes retain their existing SQL,
validation, related-row checks and lock behavior through those same helpers.
No duplicated JSON parser, source digest implementation or host-side SQL.

Build the complete bounded list before the first resource lock:

| Action | Complete resource closure and strongest needed lock |
| --- | --- |
| case.changed | Case UPDATE; its property SHARE; every current case source SHARE |
| checklist.changed | Application UPDATE; its property SHARE |
| procedure.changed | Procedure UPDATE |
| draft.saved | Case UPDATE; its property SHARE; every current case source SHARE; draft key UPDATE if present, or absence protected by the held case parent |

The existing case decoder permits 1–32 unique source IDs. Therefore the maximum
closure is 35 entries (case, property, 32 sources, draft key). Validate canonical
UUIDv7, exact finite kinds, uniqueness of `(kind,id)` and this bound before
acquisition. Sources are stored as references without SQL foreign keys; actual
existence, scope and digest are validated below, not inferred from JSON.

Sort **all** entries by UUID byte order (equivalently canonical UUID text), then
by this fixed tie order: `property`, `source`, `case`, `application`, `procedure`,
`draft`. The same UUID may exist in distinct tables; the tie rule is deterministic
and puts a case before its draft key. Distinct kinds are not deduplicated merely
because IDs match. `Kind` is a finite repository row classification, not a policy
permission or a new operation vocabulary.

Acquire each entry in that sorted order with a separate fixed scoped statement;
no multi-table locking join is an order guarantee. The native dispatcher has
literal statements for these six kinds: properties/sources SHARE,
cases/applications/procedures UPDATE, drafts UPDATE by case_id. IDs and all scope
values are parameters. It accepts no arbitrary table or lock strength. All
existing rows must be present in this exact scope, except the explicitly absent
draft key. Missing/foreign resource returns non-enumerating ErrNotFound; query,
scan, cancellation or malformed-state failure returns ErrUnavailable.

## Revalidation and callback compatibility

After the complete set is acquired, reread the selected mutation row through the
same non-locking row helper. Compare its full decoded value with discovery,
including revision and every property/source/checklist relation and meaningful
field. Any change returns ErrConflict with no list. Do not append new resources,
restart discovery or retry inside the transaction. In particular, do not call
legacy caseRecord to detect changes: it could lock a newly referenced source
before discovering the mismatch.

Only after equality succeeds, validate all held property/source rows through the
existing decoders and digest checks without new locking queries. Recheck caller
cancellation before returning a separately allocated list. Invalid stored content
is unavailable, never an empty successful closure. The actual mutable row is held
UPDATE; every referenced source/property is held SHARE through caller completion.
The existing callback methods can then reacquire only these already-held compatible
rows. Case/application/procedure revision checks, human-decision rejection and
unsent draft behavior remain unchanged. This primitive neither invokes those
methods nor allows the callback to expand its resource set.

For draft.saved, query the exact scoped draft key at its sorted position whether
or not it currently exists. The case UPDATE has already been acquired (same UUID,
case-before-draft). An absent draft has **no row lock**. Its insertion/upsert is
serialized only because every participating draft writer must lock that same case
UPDATE before touching the draft. Existing SaveDraft follows that parent rule;
qualified native callbacks must also have acquired their full ordered closure.
The returned draft entry represents that protected key, not a falsely observed
existing row. The old draft's body/references are overwritten, not read as inputs
to SaveDraft; they do not add a second source closure.

This parent protocol is cooperative. Arbitrary SQL, an incompatible importer or
an unreviewed writer can bypass it and blocks admission. Legacy mutation methods
remain callable under their historical API, but their case-then-source order is
not globally ordered prelock. A deployment cannot claim this profile's complete
lock graph while allowing such standalone writers to coexist. No old-replica
cutover, source import/retirement or provider action is authorized here.

## Foreign keys, new rows and completion limits

The inspected continuity fragment has immediate case/application→property and
draft→case foreign keys. Property SHARE is compatible with the callback's
existing parent reference; case UPDATE already protects draft parent access.
Changing those relations is outside these four mutations. Source references have
no FK; the explicit scoped read/digest check and held SHARE rows are necessary.
The fragment declares no deferred triggers. Additional installed triggers,
functions, policies or deferred work must be enumerated and qualified by the
integrator; source inspection of this fragment does not prove the deployed schema.

Each existing mutation inserts a fresh activity row. It has a scoped primary key
and no authority-row FK in this fragment. The new activity row and optional new
draft are declared callback effects beneath the locked mutation/parent, not
preexisting resources silently appended to the lock list. UUID generation or a
unique/constraint/activity failure can still fail or wait; the enclosing root
must drain and perform its final authority/F checks, roll back on precommit
failure, and release nothing before successful commit. This primitive does not
journal activity/replay/audit/effects into Root or qualify their atomic executor.
No schema change, exception to Root.Read's non-mutating use, new after-F work or
implicit authorization follows from successful prelocking.

## Verification and ownership

Source after different design CLEAR and coordinator ACK is limited to new
prelock.go/prelock_test.go/prelock_runtime_test.go plus the shared private
read.go refactor and repository README. Preserve existing public method shapes,
write.go, module, schema, root/session/policy and executable wiring. No task
acceptance/count changes or separate evidence PR.

Pure scripted-DBTX tests must show no calls for invalid selectors; no discovery
lock (especially lower-ID source before a higher-ID case); global ordering and
ties; bound/duplicate rejection; changed closure/revision returning conflict
without late-lock acquisition; malformed source digest; missing/foreign rows;
mode/cancellation/failure propagation; optional draft absence only after parent
lock; fresh output copies; and unchanged legacy relation validation behavior.
Genuine isolated negatives remove global sorting or bypass closure equality and
must fail their corresponding behavioral assertion before exact restoration.
They prove query protocol/Go behavior only, not PostgreSQL serialization.

Required-service test **source** uses the existing explicit runtime configuration
and fixture helpers, failing when absent. Separate authorized operators must
exercise four successful prelock+existing mutation/rollback paths; actual observed
resource waits; closure change committed during discovery/acquisition; ordered
multi-resource contention; existing/absent draft serialization through the case
parent; corruption/foreign-scope and cancellation/closed transaction. Actual
native authority, guarded callback lifetime, full writer coexistence and terminal
F remain integration gates. Author runtime is not granted by this proposal; no
fixture/configuration/credential access or service/browser execution is implied.
The separately blocked native regression and equivalents remain excluded.
