# Native operation journal in the existing writer Root

Status: exact additive API proposal for independent review under T2.8. No source
or dispatch grant. Read with the existing writer API, operation invocation and
transaction-current policy contracts. This is an ordering prerequisite, not an
authorizer, executor, resource locker or credential factory.

## Ownership and preserved protocol

The implementation belongs to `internal/authoritywriter`. Preserve its imports:
no identity, policy, repository, workspace or operation-package dependency. Use
the existing Root and Attempt, original five-second request budget, singleton G
then database B, ordered P/C/H/S/W/D/F phases, actual constraint drain and exact F.
Never create a second transaction owner or parallel operation phase machine.
Existing `NewPlan`, identity journals, delivery and counter-only paths retain
all behavior. `Root.Read` remains non-mutating. New operations use a disjoint
explicit operation profile on the same Root, not a reinterpretation of an
identity Success or CounterOnlyDenied transition.

## Exact additive declarations

```go
// All fields are selectors/declarations from trusted native composition, not
// authority. IDs must be UUIDv7 in the Root realm.
type OperationPlan struct {
    InvocationID uuid.UUID
    ActorID uuid.UUID
    SessionID uuid.UUID
    WorkspaceID uuid.UUID
}
func NewOperationPlan(realm Realm, rows []Row, operation OperationPlan) (Plan, error)

type OperationStep uint8
const (
    OperationAuthority OperationStep = iota + 1
    OperationResources
    OperationClaim
    OperationCallback
    OperationResult
    OperationAudit
    OperationReplay
    OperationDeniedAudit
    OperationUnavailableAudit
)
func (a *Attempt) OperationTx(context.Context, OperationStep) (*sql.Tx, error)
func (a *Attempt) CompleteOperationStep(context.Context, OperationStep) error

// Append after the existing Outcome constants, preserving their values.
// These outcomes are only legal for the new operation profile.
const (
    OperationDeniedCommitted Outcome = UnavailableRollback + 1
    OperationUnavailableCommitted Outcome = UnavailableRollback + 2
)
```

`NewOperationPlan` uses and copies the existing finite Row plan, rejects delivery,
and stores a copied nonzero operation selector. It requires exactly one existing
UPDATE Person, Session and Workspace matching the respective IDs; optionally one
existing UPDATE Membership, and no other identity rows or reserved inserts.
The selected Membership is still a selector: native workspace recheck must prove
its association. InvocationID must not equal any other selected ID. A normal
NewPlan cannot call the operation methods. This profile never issues, renews,
revokes or changes credentials, sessions, people, workspaces or memberships.

The operation Root's P/S/W row locks precede all later native classes. Billing
and complete domain prelocks belong to the native OperationAuthority/Resources
steps in the same lexical transaction. Their concrete implementations and
compatible writers remain separate mandatory qualification gates. The Root
journal does not infer a complete domain closure from a row count, IDs, successful
SQL or a caller saying it is authorized.

## Sequential native custody

`OperationTx` is privileged native plumbing, like the existing ParticipantTx;
it is not exposed through InvocationContext, a host option or any business API.
It accepts no SQL, function, provider, type-erased payload, facts map or replacement
transaction. It returns only the current Root's exact SQL transaction. This is
not a SQL sandbox: independently reviewed native call sites are responsible for
using the returned handle only for the declared step and never retaining it in
business code. A callback receives only the existing guarded operation.DBTX.

Both methods call the existing live/profile/original-context/deadline guards.
The first step requires phase W, a sealed operation plan, no mutation journal,
no counter restriction and no delivery state. It enters D without extending the
budget. There is at most one open step. Opening another, completing a different
step, completing twice, skipping a required step or mixing a legacy mutation or
delivery method poisons the attempt. A failed native step is not completed;
caller terminates with DeniedRollback or UnavailableRollback. No retry occurs.

The legal paths are exactly:

- New invocation: Authority -> Resources -> Claim -> Callback -> Result -> Audit.
- Authorized replay: Authority -> Resources -> Claim -> Replay.
- Denied attempt: Authority -> DeniedAudit, or Authority -> Resources -> DeniedAudit.
- Unavailable attempt with usable transaction and trusted attribution:
  Authority -> UnavailableAudit, or Authority -> Resources -> UnavailableAudit.

Each arrow requires completion of the preceding step. Authority may complete
with a denied/unavailable policy result solely to enter its audit-only path;
completion itself does not mean Allowed. Claim may produce a new claim, an
existing replay or a conflict; native code selects Callback or Replay only for
the corresponding checked result. A conflict uses DeniedRollback or
UnavailableRollback according to the existing private error mapping, emits no
false policy-denial event and releases no output. The public operation conflict
remains a conflict; Root rollback markers are not the public policy vocabulary.
An unusable transaction never attempts audit-only commit.

**Post-Claim denial is an explicit unimplemented integration dependency.** A
current-authority refresh after a capacity/duplicate wait may deny or become
unavailable after Claim has staged reservation/pending-invocation rows. This
finite journal cannot then enter either audit-only terminal path: doing so could
commit those rows while claiming an audit-only outcome. Its only safe available
terminal behavior is whole-attempt rollback with no output, no Completion and
no claim that a denial event was recorded. That behavior does not satisfy the
full frozen executor's audit requirement and therefore blocks dispatch for that
executor. No implicit savepoint, second transaction, reservation compensation,
retry or relabeled success is allowed. A separately reviewed exact native unwind
or other owner-approved atomic denial protocol must close this gap before
operation dispatch; implementing this ordering prerequisite does not close it.

Authority includes same-instance session provenance/current checks, refreshed
principal/workspace handoff, policy and billing facts. Resources acquires and
revalidates complete ordered native closure. Claim owns ordered actor/workspace
capacity then idempotency; no installation allocator. Both steps are provisional:
current authority must be refreshed after waits before Callback or Replay.
Callback includes guarded domain mutation and qualified durable effect enqueue;
Result happens only after guard invalidation/drain and owns canonical output
validation plus replay completion/capacity shrink. Audit appends the one success
event last. Replay validates exact stored bytes/schema and buffers them without
a callback or second success audit. DeniedAudit/UnavailableAudit append exactly
one finite attempt event with trusted context and no claim/domain/effect work.

The journal enforces path order and lifetime, not the truth of these native
obligations. In particular a raw SQL handle cannot reveal whether an adapter
performed an undeclared write or took a lock in the wrong order. Therefore no
operation dispatch or acceptance follows from implementing/testing this API;
actual fixed call sites and complete native adapters are mandatory.

## Terminal drain, outcomes and release

`DrainAndSample` on an operation plan rejects an open or incomplete step and
requires the completed terminal step Audit, Replay, DeniedAudit or
UnavailableAudit. It then uses the existing drain and exact one F, unchanged.
No operation method or legacy participant/mutation/delivery API can acquire a
new handle or perform a transition at or after F. Native finalizers may use
already-retained lexical handles for existing permitted plain final SELECTs;
no new locks, mutations, clocks, codec, renderer or provider work occurs.

`Finish(Success)` requires completed Audit or Replay and F. The two new committed
outcomes require their corresponding single audit-only path and F. They cannot
be used by legacy plans, and CounterOnlyDenied cannot be used by an operation
plan. Rollback outcomes remain available before or after F as appropriate.
Raw invocation success or Finish never proves the final policy/session checks.
The actual native finalizer and operation result release bridge remain separate
mandatory integration dependencies.

Root.Run commits the two new outcomes only after matching finished state,
unchanged original request checks and successful runtime commit. Extend
Completion/Release's finite committable check to these outcomes without changing
their same-binding and one-use semantics. No credential Permit is created;
existing writerproof.Permit continues accepting only its existing Success or
CounterOnlyDenied outcomes and exact matching release. A denial release can
therefore release only the operation adapter's bounded generic denial, never a
credential or protected result. Failed/unknown commit or request cancellation
returns no completion; unknown commit does not prove no audit event exists.

## Explicit source and verification scope

After different early review and coordinator assignment, source owns only the
new operation journal files and additive plan/attempt/root changes plus package
README. No schema, exported app executor, policy implementation, identity proof,
provider, registry or host activation is included. New plan fields stay private
inside copied Plan data. Existing normal plan behavior must remain unchanged.

Pure tests must cover every allowed path and rejected skipped/repeated/mixed
transition; wrong/canceled contexts, wrong/closed attempt, legacy profile,
plan-copy isolation, required selector/row exactness, pending-step drain gate,
terminal outcome mismatch and one-use matching releases. No fake transaction
may execute SQL or count as runtime evidence. A targeted guard-removal negative
must fail genuinely and pass after restoration.

Subsequent independently operated required-service checks must use a fresh
qualified synthetic runtime, actual Root.Run and database rows: native journal
steps on one backend, rollback versus audit-only commit, actual final drain,
unknown/failed completion suppression, callback/row lifetime composition and
compatible authority/resource/replay/audit ordering. Missing prerequisites fail
visibly. This test plan does not include the separately blocked native assurance
regression, any equivalent probe or a containing whole-native suite.

Full operation policy/current-session F receipts, billing invalidation and
permission mappings, complete repository prelock and callback compatibility,
atomic effects/replay/audit, migration history and owner concurrency, all native
writers and whole B1/provider/release gates remain open. This proposal adds no
task, acceptance or public authorization surface.
