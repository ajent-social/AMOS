# Transaction-current operation policy: composition proposal

Status: early design for independent review, not an implementation grant or
permission to dispatch operations. This proposal preserves the complete
[operation contract](operation-invocation.md), including its frozen v1.13
interfaces, and the [W1 completion contract](current-session-writer-api.md).
It does not amend either contract by implication. The integrator must separately
assign every missing native adapter below after its design is reviewed.

Source baseline: `0c1e3976dc00c5d1ee0f2ab804597486456e025d`. The additive
workspace context implementation at
`f32bb3e5b4847ea6a0fcf3308a489d017e1684df` is considered separately; it is not
present in that baseline. References below describe source capabilities, not
runtime qualification. Full SaaS, organization, identity, billing, provider,
portability and release requirements remain in scope.

## 1. Preserve the public decision boundary

Keep `policy.Evaluator.Evaluate(ctx, principal, resource, requirements) Decision`
and the specified `operation.TransactionAuthorizer.RecheckCurrentTx(ctx, tx,
principal, selection, resource, lockSet, requirements) Decision` unchanged.
The latter remains a frozen design surface, not an implemented interface in
`app/operation` at this baseline. Only
`policy.Allowed`, `policy.Denied` and `policy.Unavailable` are policy outcomes.
A resource or requirement is a selector/declaration, never authoritative evidence.
Do not introduce a second permissions table, entitlement vocabulary, transport
policy, arbitrary authority callback or exported context/facts setter.

The proposed concrete authorizer and evaluator live together in the **private
native composition**, above the leaf `policy` contract. For the continuity
composition this means private types in `examples/continuity/host`; a reusable
native composition can later own the same implementation with an explicit path
assignment. `policy/contracts.go` stays a leaf. In particular, do not import a
repository that consumes `app/operation` into the root `policy` package and
create `policy -> repository -> operation -> policy`.

The authorizer owns a per-invocation private frame, not a shared mutable evaluator
or a process-wide map keyed by a request ID. The frame binds the exact native
composition instance, original request lifetime, exact transaction, original
admitted principal, refreshed principal, pinned workspace and copied selection,
authorization resource, complete copied lock set, copied registered requirements,
policy revision, locked billing/resource facts and the actual database sample.
Only its concrete native acquisition path can construct it. No public constructor
accepts a map of asserted grants, selected owner ID, projection, clock or resource
predicate. Business code and transport input never receive the frame.

A private evaluator implements the frozen interface and consumes that frame only
within its owning authorizer call. It rejects absent, closed, wrong-instance,
wrong-transaction or mismatching input; it performs no SQL, provider call or
callback. Returning a Decision neither exports the frame nor makes it reusable.
Invalidate it on transaction completion, failure or cancellation. Context values
preserve cancellation and native session proof; a copied context cannot establish
freshness in a second transaction. Pure internal test facts qualify only decision
logic, never native admission or transaction provenance.

## 2. Exact database, session and fresh-value custody

The native constructor opens one `*storage.RuntimeDB`, retains it privately,
constructs one `*authoritywriter.Root` from that exact pointer, and constructs
its private `*session.Service` with `session.NewWithWriter` and that exact Root.
Middleware and recheck use the **same service object**. The pool-free
`workspacecontext.NewForTransactions` receives the copied realm. Billing and
domain adapters receive only the transaction from this Root's current invocation;
none opens a pool, selects a database, or begins a nested transaction.

No public configuration slot supplies a RuntimeDB, session service, transaction,
TxRunner, authorizer callback or prebuilt facts. Private construction and reviewed
call sites prove this custody. Matching realm IDs, database names, backend IDs
or copied rows do not prove the origin of an arbitrary `*sql.Tx`. The low-level
session API cannot detect a different database containing identical rows.
The existing private read core demonstrates the construction shape, not a writer
host. `apphost.NewLocal` and legacy workspace middleware are not substitutes.

The original middleware context reaches every session check unchanged in proof
identity. `RecheckCurrentSampleTx` returns both a fresh principal and its actual
DB instant. It performs the native person/session checks; the policy adapter
must not copy its cookie parser, SQL or assurance case table. Use
`ResolveCurrentTx` with that refreshed principal and the pinned workspace ID;
compare current ownership, workspace/member identities, epochs and permissions
to the original trusted selection. Never switch a default selector to a newly
chosen workspace during completion.

The frozen authorizer returns **only a Decision**, so it cannot silently convey a
new principal or selection to the executor. This is a concrete integration gate.
The proposed native per-invocation assembly retains those refreshed values in its
private frame and supplies them to the handler's `InvocationContext` through a
separately reviewed concrete executor composition seam. No public fact getter or
context principal replacement is proposed here. Until that seam exists, any
changed principal/selection must prevent dispatch; the executor must never take
an Allowed result and invoke a callback with its old snapshots. This proposal
does not claim that a conservative rejection implements the full refresh profile.

`ResolveCurrentContextTx` is useful for attribution only while the original
context principal fully equals the supplied refreshed principal. Its different
reviewed contract requires a fresh resolution and independently copied private
selection binding; it is not an arbitrary Selection setter. If refresh changes
assurance or any compared field, it returns denied and no context. Do not weaken
that check or forge a principal to pass it. Attribution for the general refreshed
case needs a separately reviewed native bridge. Audit attribution, even when
valid, is not permission to perform the audited action.

## 3. Current workspace and permission facts

Reuse `workspace/context/current.go`: person SHARE, exact workspace SHARE, then
organization membership SHARE and the selected immutable role version. A missing
or revoked current person/workspace/member is denied; SQL/mode/malformed role
state is unavailable. Missing-row serialization relies on the complete W1
person-UPDATE writer protocol and no-old-replica gate. A SELECT returning no row
is not itself a predicate lock. All writers of the relevant facts must be
qualified before any dispatch relies on this protocol.

The existing organization resolver validates exactly these installed permissions:
`workspace.members.read`, `workspace.members.manage`, `workspace.owners.manage`,
`workspace.authentication.manage`, `workspace.delete`, `billing.manage`, and
`billing.read`. Evaluate exact membership in the current copied permission set;
do not infer wildcards, implied administrator privileges or business grants from
role names. A supported required permission absent from current authority is
denied. An unimplemented permission/role adapter is unavailable and blocks
registration for that profile; do not invent business permission strings here.

A personal selection currently has no membership or permission slice. The
persisted personal owner relation proves ownership of that workspace, not every
possible business permission. A personal action needs an explicit reviewed
mapping from the existing operation requirements to current owner facts. Until
that mapping exists its writes are unavailable, including operations declaring
no permissions: an empty requirements array is not an owner-policy bypass.
Organization authentication/assurance requirements must also come from their
current native policy adapter; payment or an ordinary AAL1 session cannot waive
organization MFA/enterprise policy. That adapter remains a dependency.

The private host's `personalRead` supports only the separately declared source
and baseline read profile. It is not `policy.Evaluator`, not a write authorizer,
and cannot be reused as permission for operations, drafts or external effects.

## 4. Billing acquisition and temporal evaluation

Reuse `billing/catalog.Catalog.Evaluate` for its supported immutable flat-price
catalog semantics. Its `Scope` includes application as well as installation,
environment, workspace, provider/account/mode; subscription projections must be
resolved from this exact trusted scope. Neither catalog configuration nor a
provider customer ID grants tenant permission. Essential account/recovery/billing
restoration paths retain their separate permission policy; they are not a generic
business-operation exception.

A **missing native billing adapter** must read and lock the exact active billing
account, current selected projection set and invalidation/reconciliation state in
the invocation transaction, before domain/capacity locks. It must resolve complete
product-family selection, absent/multiple projections, pending invalidation and
provider binding under a compatible writer protocol. Projection absence requires
a protected parent/uniqueness predicate, not an invented row lock. Put this SQL and
row decoding in the billing owner, not the host or evaluator.

`billing/store.New(tx)` is transaction-bound, but its existing
`FindCustomerBinding` is an unlocked customer lookup and
`UpsertSubscriptionProjection` is a writer. Neither supplies this current reader.
Reconciliation locks its work row before applying projections; callback/dirty
version invalidation, account state changes and projection replacement must be
included in the full graph. Adding a projection SHARE query alone does not qualify
these paths. Do not call a provider to repair freshness inside an invocation.

At the actual authorization sample, a positive projection must be non-future and
strictly before the earliest of its observation plus configured freshness (at
most five minutes), paid-through boundary and any applicable trial/grace boundary.
Reevaluate at the terminal F instant. Equality is expired. A stale positive,
pending invalidation, missing/ambiguous projection or unsupported lifecycle/model
is unavailable, never a stale allow. Fresh established non-entitlement is denied.

There is a concrete mapping difference to resolve: the current catalog reports
`outside_paid_period` as denied before its stale-projection check. The operation
contract requires a formerly positive projection crossing its financial boundary
to be unavailable. The native adapter must apply the operation freshness fence
before consuming an otherwise positive historical projection; it must not globally
reinterpret every catalog denial as unavailable or edit the catalog silently.
Preserve the catalog's exact supported state/price/feature checks after that fence.
Seat, metered, trial/grace and full lifecycle adapters are still required for their
product paths; the current flat/USD component does not qualify them.

For Allowed, `EvaluatedAt` is the actual database sample; `ExpiresAt` is the
minimum proven session/assurance, billing and policy validity bound, strictly
after the sample. No application clock, arbitrary TTL or infinity supplies a
missing bound. The session reader's returned principal bounds assurance but does
not expose its full idle-expiry receipt; a native sealed final-session receipt is
therefore another prerequisite for a complete operation validity bound. Policy
revision binds the immutable operation policy and relevant installed role/catalog
revisions; a caller cannot select it. Denied timestamps are also database-derived;
Unavailable never fabricates an evaluation instant or safe retry promise.

## 5. Domain ownership and complete resource closure

The typed operation resolver supplies untrusted resource selectors after canonical
input validation. It is not the owner validator. Authorization resource must be
in a bounded complete set, with exact workspace equality, unique IDs and the
frozen global ascending UUID order. A closed native adapter dispatches only
reviewed resource types to their owning store; unknown types are unavailable.
No string-to-table interpolation, generic SQL predicate or callback decides
resource tenancy.

The continuity repository already owns all four scope predicates, strict JSON,
revision and source-digest validation. `NewWithDBTX` adapts the callback capability;
it does not authorize a scope or prelock resources. Its public Case/Application/
Procedure reads do not implement the complete ordered prelock seam. In particular,
`caseRecord` locks a case then its referenced sources, `application` reads its
property, and `SaveDraft` writes under a case parent. That method-specific order
is not proof of global UUID order across all affected resources.

A separately assigned repository-native prelock adapter must discover the exact
closure (including referenced sources, property parents and draft parent/key),
acquire existing rows in the declared global order, then reread and validate the
relations under those locks. If closure changes, a dependency is absent, an
absence predicate lacks protection, or a write would acquire an unplanned lock,
fail closed before capacity claim/callback. Do not repair a changed closure by
appending a lower-ID lock after higher-ID acquisition. Missing/foreign resources
use the existing non-enumerating denial; corrupt state is unavailable.

Existing repository decoding/query helpers stay the single owner implementation.
No host-side copy of the source digest parser or scoped SQL is permitted. Callback
methods may reacquire only already-held compatible rows and create declared new
children beneath locked parents. Activity insertion, reference reads, deferred
constraints and draft upserts belong in the complete lock/dependency graph.
Source import/retirement is not admitted by the immutable-source read profile.
Human decision and unsent-draft restrictions remain unchanged.

## 6. Lock order and terminal completion

For normal operation authority the frozen order remains:

1. Current person/account, authenticated session.
2. Selected workspace, membership and selected immutable role/permission facts.
3. Billing account, current projections and their qualified invalidation fence.
4. Complete domain resource set in ascending UUID order.
5. Actor capacity, workspace capacity, idempotency row.
6. Domain mutation under the held set; durable intent/job rows; invocation audit
   last, after result completion, with all declared constraints drained before F.

Within each class acquire multiple rows in ascending UUID order. A W1 writer Root
also acquires its existing singleton G before discovery/person work; nothing may
acquire G after an operation's resource or capacity locks. Normal operations
never take the installation capacity allocator; owner allocation/increase keeps
its separate installation-before-actor-before-workspace order.

**The existing W1 Root cannot execute this sequence as-is.** Its finite Plan
stops at identity/workspace rows; D admits only the sealed material/job delivery;
its mutation journal has no domain, replay, operation effect or invocation-audit
participant. Its outcomes do not include a policy-denial audit-only commit.
`CounterOnlyDenied` means the exact MFA counter update and must never be reused
for that event. `Root.Read` is non-mutating trusted plumbing, not an escape hatch.
A native operation-root extension needs its own explicit finite participant,
journal, audit-only outcome and ordering design/review before implementation.
Preserve the original Root five-second total budget (including pool/G waits),
G then database B, and ordered P/C/H/S/W/D/F phases, including empty phases.
Neither a policy call, repeated recheck nor a later database sample resets it.
The operation-specific classes above must be mapped into a reviewed extension
of this protocol, not implemented as an independent alternative phase machine.

Proposed sequence for that future native operation root:

- Acquire the complete authority/resource set, collect private facts, obtain the
  native session sample and evaluate provisional policy before claiming capacity.
- After capacity or duplicate-row waits, refresh through the original same-service
  session and current adapters using only already-held compatible authority rows.
  Reevaluate before callback or replay disclosure. No cached Allowed is authority.
- For a new write, run the real guarded callback, invalidate/drain its handles,
  validate and privately buffer output, complete replay/capacity, persist effect
  intent and append success audit. For replay, validate stored bytes/schema and
  buffer them without callback or a second success event. All work is provisional.
- Refresh locked non-temporal facts before the final drain. Run the existing root
  constraint drain and take its single recorded F. At F, concrete native
  finalizers perform the W1-required plain-read comparisons of already-held
  authority rows and intended transitions using their original lexical
  transaction. They evaluate session/assurance, billing and policy against
  **that exact instant**. They acquire no new lock, take no new clock sample,
  mutate nothing, and call no provider, renderer, codec or application callback.
  Finish and commit follow. Failure rolls back all staged mutation/result/audit/
  effect work and releases no output.

`RecheckCurrentSampleTx` cannot be called after `DrainAndSample`: it executes SQL
and takes another clock sample. The native identity owner must supply a sealed
held-row receipt/final check tied to the original private admission and attempt,
reusing its existing validation logic, before this design can compose F.
`session.CheckStagedWriter` demonstrates a native plain-read final fence using
private `readCurrentAt(..., false, f)`; it checks a staged issuance and is not an
existing current-operation finalizer. The existing writer evidence is not an
arbitrary operation receipt. The authorizer's
frozen public method remains the provisional acquisition/evaluation boundary;
a private native finalizer is a separate mandatory integration dependency, not
an overload that resamples after F. Where native issuance is involved, preserve
the existing `writerproof.Permit`, `writerproof.Finalize`, `Root.Finish` and
`session.PublishWriter` sequence, including exact `Completion.TakeRelease` /
`Permit.MatchesRelease` binding and once-only publication. There is no new
`NativePermit`, `FinalizeWriter`, generic credential permit or callback that can
replace these checks. The non-issuing operation output/release bridge remains
unimplemented and needs an exact separately reviewed API and native ownership;
an audit/store transaction test does not establish that bridge.

W1 defines F as the freshness linearization instant conditional on successful
commit, not network-receipt time. The older operation wording says an Allowed
expiry passed before commit must roll back. That wording cannot be certified as
a stronger commit-time clock promise by this design: the integrator must resolve
it explicitly against W1 before operation dispatch is admitted. This proposal
recommends the existing F semantics and does not silently amend the frozen
operation contract. No extra SQL clock is inserted between F and commit.
Cancellation and failed/unknown commit suppress output; unknown commit is not
proof of rollback and does not authorize automatic retry. Persisted timestamps
retain the operation store's transaction-time semantics. Audit-only denials need
their own reviewed terminal path with trusted attribution; an unusable transaction
or failed audit commit returns unavailable without claiming an event exists.

## 7. Finite implementation and qualification gates

This design can first be checked as a source dependency specification. It does
not authorize implementing an always-unavailable replacement and calling policy
complete. The integrator must assign these concrete deliverables before source:

| Owner | Missing deliverable / gate |
| --- | --- |
| Native composition / operation | Concrete per-invocation frame, frozen-interface adapter, refreshed InvocationContext handoff and private terminal finalizer; same RuntimeDB/session custody |
| Identity / W1 | Sealed current-session final receipt, full original admission binding, root operation participants/outcomes and F integration; complete supported producer/writer graph |
| Workspace / policy | Reviewed personal action mapping, organization policy and installed business permission adapter; no manufactured permissions |
| Billing | Locked account/projection/invalidation reader, compatible complete writers, catalog outcome mapping and financial boundary checks |
| Domain repository | Complete closure discovery, ordered prelock/revalidation and callback compatibility using existing decoders |
| Operation / audit / jobs | Atomic replay/capacity/result/audit/effect integration, audit-only denied/unavailable path and guarded callback lifetime |
| Integrator | Migration/history/role/namespace qualification, full native composition, immutable transport/codec bindings and admission checks |

Different early design review is required before any implementation, followed by
explicit path ownership; this document delegates no `policy/**`, identity,
workspace SQL, repository, module, schema, wiring or acceptance edits. Unsupported
paths stay unavailable and registration/activation remains closed until required
adapters exist. Missing dependencies must not be described as policy denial.

Required subsequent evidence includes actual native admission with the same
service/runtime, cross-instance rejection and a composition-level two-database
injection negative; locked current membership/permissions and complete domain
closure; real billing invalidation/freshness/financial boundaries after observed
waits; no stale principal passed into a callback; replay reauthorization; rollback
of domain/replay/audit/effects; and the terminal F/commit/publication boundary.
Use real required-service checks with visible failure when absent, plus genuine
negative/restored source tests where authorized. Fixtures do not qualify providers.

The separately platform-blocked native assurance regression and equivalents remain
blocked: no retry, rename, substitute implementation or containing full-suite run
is authorized by this proposal. Ordinary policy/storage/browser tests do not close
that coverage gap. Existing component evidence, unavailable hosted CI and full
product acceptance remain distinct; this design changes no task status or count.
