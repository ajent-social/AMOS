# Same-transaction workspace selection for B1 reads

Status: proposed CP4 source contract within the authenticated replacement batch.
No source, API adoption, operation-executor or host qualification follows until
independent exact-head review. Read with the current-session recheck, W1 writer
and single-runtime composition proposals. The complete organization, billing,
provider and release scope remains in the original product plan.

## Shared owner and exact seam

Extend the existing `workspace/context` owner; do not copy its selection SQL into
a business app or compare a user to a configured owner identifier. Preserve the
legacy constructor/middleware and its private `resolve` helper at their historical
boundary. Add a DB-free constructor and caller-transaction method:

```go
func NewForTransactions(cfg Config) (*Resolver, error)
func (r *Resolver) ResolveCurrentTx(ctx context.Context, tx *sql.Tx,
    principal identity.Principal, workspaceID uuid.UUID) (Selection, error)
```

Config and Selection keep their existing shapes. The new constructor validates
and copies the realm without a pool, runner, callback or session dependency.
The method accepts only a real person principal for its exact realm; machine
actors fail unavailable until their qualified grant adapter exists. A zero
workspaceID selects the person's current personal workspace through the existing
owner relation; a nonzero UUIDv7 is an untrusted exact selector. Invalid selectors
are invalid, established missing/foreign/inactive authority is denied, and SQL,
mode, malformed-state or dependency failures are unavailable with zero selection.

This method supplies current locked workspace facts, **not session provenance**.
Only the trusted composition may combine them with the principal just returned
by its exact private session service's RecheckCurrentTx. A Principal or Selection
passed directly to this method cannot establish a current session. No new public
context setter or credential parser is added. Returned slices/member pointers
are copied; retaining or modifying them never changes database authority.

## Row and predicate protocol

The caller owns the single-runtime explicit writable READ COMMITTED transaction.
ResolveCurrentTx independently checks mode/cancellation and locks the exact scoped
person SHARE, validating current active state and security epoch against the
supplied refreshed principal. In the composed sequence this only reacquires the
compatible person lock already held by session recheck, never an upgrade.
All SQL added for this seam qualifies application objects with public and built-in
functions with pg_catalog, under the reviewed immutable namespace profile.

For zero selector, discover the exact personal workspace through the existing
unique owner relation, then lock that exact scoped workspace SHARE in a separate
statement and revalidate kind, immutable owner, state and nonnegative epoch.
For an explicit selector, lock that exact workspace SHARE directly, then validate
the same fields. A valid personal selection is based on current database ownership
of the authenticated person. It is not a configured owner-ID bypass.

For an organization selection, lock the exact realm/workspace/person membership
SHARE in a separate statement after workspace. Validate active state, epoch and
role/version, then read that immutable role-version's permissions. Missing role
or malformed permission data is unavailable, not a successful empty grant. Return
copied current membership/permissions/epoch. The finite permission vocabulary is
the installed immutable role schema; it does not imply permission for an arbitrary
business operation. Unsupported/unknown roles or workspace kinds fail unavailable.

For absence predicates, the held person SHARE is meaningful only under the complete
W1 protocol: every membership creation/move/state or person-binding writer must
hold the complete affected person set UPDATE before W. Positive workspace rows
remain SHARE-held; state writers conflict there. Role versions are immutable and
selected through the locked membership. No claim of predicate locking follows
from a plain SELECT or a missing row alone. Old writers with weaker person locks
must not coexist with this selected profile; the W1 no-old-replica gate remains.
No resource can be returned while only a subset of this writer graph is qualified.

Repeated resolution with the **same resolved workspace ID** may reacquire held
compatible rows after downstream waits. It cannot silently switch a default
personal selection to another workspace at completion. A different selected ID,
workspace epoch, member identity/epoch or permission set invalidates the provisional
result. The native composition explicitly forwards both the refreshed principal
and fresh selection; it never continues with the original context snapshots.

Existing owner-resolution and row-decoding logic stays in workspace/context and
workspace/store. Refactor shared private query/validation helpers as needed; legacy
plain reads and new ordered locked reads use the same owner semantics. Do not
change legacy callers to require W1 or silently add a pool to the new resolver.
No schema change or new role is required by this selection seam.

## Finite initial continuity resource policy

The first source list/detail read is explicitly the current **personal** workspace
profile of the preserved full product. An organization selection returns a safe
unavailable response in that host until its registered operation permissions and
entitlements have a qualified evaluator; it must not fall through to personal
policy or be described as full organization support. This is a declared initial
host profile, not retirement of the remaining product tasks.

For this profile, source rows are visible only through the selected repository
Scope derived from the refreshed realm/workspace. Existing Sources/Source own
literal bounded search, UUID validation, row scope and digest validation. No extra
SQL search, source model or credential parsing lives in the host. Unknown/foreign
detail is a non-enumerating not-found response; corrupt digest, unavailable store
or unestablished policy is unavailable, never an empty success. Source content is
immutable reference input in this initial profile; no import/update/delete writer
or application role grant for source mutation is admitted. Provisioning/import
and any future source-retirement authority need a separately reviewed writer and
resource fence before enabling them. This excludes concurrent privileged source
changes from the profile; it does not pretend SELECT protects against them.

Business writes, local edits, unsent drafts, replay, audit and durable effects
still require the shared operation contract and executor. This read policy cannot
be reused as an unregistered mutation path. A source screen does not complete
RB1-RB9, WEB/HOST or replacement acceptance.

## Completion and required evidence

The trusted read flow is: original request admitted by exact session middleware;
Root.Read on the private runtime; session recheck; ResolveCurrentTx; finite policy;
repository retrieval and escaped bounded render into private bytes; session
recheck after all waits; ResolveCurrentTx for the pinned ID; reevaluate policy
using refreshed values; successful commit; only then publish buffered bytes.
Before the final sample, drain all declared deferred work even if this callback
is logically read-only. No SQL/domain callback follows final authority evaluation.
F is the final database-time validation under held authority locks, conditional
on successful commit, not a promise about network receipt time. The separately
reviewed HTTP contract owns status/headers, bounds and failure publication.

Required real-service cases include personal default/explicit selection;
foreign/inactive/absent selection; organization active/removed/suspended member;
missing or malformed role state; current person/epoch mismatch; wrong mode,
closed transaction, cancellation and query failures. Observe both lock orders
with actual W1 state/membership writers. After a resource wait across session or
assurance expiry, prove refreshed principal/selection reach the consumer and no
old snapshot grants disclosure. Test failed commit/rollback and exact scope/digest
retrieval, without re-running already completed primitive checks as new evidence.

Genuine negatives omit a current membership/state predicate, switch back to the
old principal, or bypass the post-wait recheck; each must fail its intended denied
or no-disclosure assertion, then pass after exact restoration. Same-database
custody remains a composition review and actual two-handle injection negative,
not a property the supplied sql.Tx can prove itself. Runtime fixture source/schema
requires different exact review and a fresh serial grant before use.

Ownership: workspace/context and store under T4.4/T4.2, shared composition under
integrator/T2.8, source retrieval under T-RPL-SEARCH.2. No foreign identity source,
module, migration, executable or task acceptance edit is delegated by this proposal.

## Proposed binding for transaction-owned audit attribution

The transaction-only resolver currently returns facts without attaching its
private selection context. The legacy middleware owns a different database
handle and cannot be substituted into the private runtime composition. Add this
finite method to the existing resolver, subject to independent design review:

```go
func (r *Resolver) ResolveCurrentContextTx(ctx context.Context, tx *sql.Tx,
    principal identity.Principal, workspaceID uuid.UUID) (context.Context, Selection, error)
```

This is a resolving operation, not a setter accepting a caller-supplied Selection.
It first requires the native principal already in ctx to match the supplied
freshly rechecked principal in every exposed field: installation, application,
environment, person, security epoch, authentication method, authentication time,
actor kind/person/machine IDs, assurance level and assurance expiry. Compare
instants with time.Time.Equal. Missing or mismatched principal is denied without
SQL or a bound context; nil/canceled context and missing dependencies remain
unavailable. It then calls the existing ResolveCurrentTx exactly once with the
same ctx, tx, principal and selector. Propagate its precise error class and return
nil context plus zero Selection on every failure. Recheck cancellation before
binding. On success attach a separately copied selection under the existing
private key; return another independently copied Selection. Caller mutation and
FromContext mutation must not change the stored snapshot. Preserve the original
context's cancellation and values. Add no pool, credential parser, public raw
context setter, permission callback or SQL query outside ResolveCurrentTx.

The private composition must still supply its exact native session recheck and
retained transaction. This method establishes neither session provenance nor a
permission grant, and a retained context is not valid current authority on a
later transaction. Invoke it before final authority sampling, retain the resolved
workspace ID, and repeat current rechecks after waits as required by the owning
operation contract. It is not a replacement for the complete operation evaluator,
writer graph, final validation or commit-before-publication gates. In particular,
the audit writer may use the bound attribution to record a denial without granting
the attempted operation. Legacy middleware and ResolveCurrentTx stay unchanged.

Required evidence includes missing/mismatched principal rejection before SQL,
error/cancellation propagation, native admitted personal selection attached on
the same retained transaction, independent copies of membership/permissions, and
actual invocation audit attribution from that bound context. These checks do not
qualify an executor or any excluded authority regression. Ownership is the
existing T4.4 workspace/context path, with integrator-owned contract and private
composition; no foreign identity, magic-link or MFA path is delegated.
