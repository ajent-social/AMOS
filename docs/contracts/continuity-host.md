# Continuity persistence and local composition, revision 1

Status: proposed; independent exact-head adoption required before source.
This contract separates a transaction-scoped application repository from its
later authenticated HTTP composition. No new framework authority API is adopted.

## First source slice: persistence mechanics only

`examples/continuity/repository` owns typed application storage. It accepts an
explicit caller-owned `*sql.Tx` and a validated `Scope` containing installation,
application, environment and workspace UUIDv7 IDs. Scope is a selector, never
authority. A nil context/transaction or invalid scope fails before SQL. The
repository never opens a database, commits, rolls back, retries, resolves browser
credentials or constructs a principal. Its results are provisional until the
caller commits; no response may escape a failed commit.

Repository source has no exported HTTP handler or generic operation registration.
A later caller must supply qualified current authority and own the complete
transaction. Test access to a repository is not proof of an authorized service.
Application policy remains in the app; shared identity and execution mechanisms
remain in AMOS. Registered operations must use the frozen shared executor.
Custom native handlers require separately reviewed session, origin/CSRF and
resource checks under the host-routing contract; they cannot bypass a registered
operation's admission rules.

The application owns `examples/continuity/migrations/continuity.sql`. It is an
unallocated append-only SQL fragment, with no registry export or automatic
application in this first slice. The integrator allocates its final position
only after comparing the target application's immutable migration history.
Tests may provision this exact new fragment into an isolated fresh fixture;
that does not authorize installing or replacing an existing schema.

### Data and transaction rules

Use separate `continuity_properties`, `continuity_sources`, `continuity_cases`,
`continuity_applications`, `continuity_procedures`, `continuity_drafts` and
`continuity_activity` tables. Every row carries all four scope IDs; every query,
CAS predicate, uniqueness key and cross-record relationship includes them.
Resource IDs are UUIDv7 minted by the authorized application, never inferred
from display names. Display owner/occupant labels confer no authority.

Properties contain only bounded display name, area, owner/occupant labels,
occupancy enum and optional inspection date. Sources contain a finite kind
(`correspondence` or `document`), title, plain-text body and exact UTF-8 body
SHA-256. Case/application/procedure values follow the adopted domain contract.
Source references must resolve in the same scope before a case or draft can be
stored or disclosed. Unknown or foreign IDs are indistinguishable NotFound.

All mutations use expected positive revision predicates, increment once, and
store an activity record in the same caller transaction. The repository uses
the pure domain functions to compute permitted changes; callers cannot supply
a human-decision update or unvalidated next value. Missing records return
ErrNotFound; same-scope stale revisions return ErrConflict; malformed data
returns ErrInvalid; all unexpected database failures become ErrUnavailable.
Return zero outputs on failure. No raw SQL/provider diagnostics are public.

A draft is an unsent body and source snapshot bound to the current case revision.
A unique scope/case key allows only one saved draft for that case. Saving a draft
locks the current scoped case, verifies the caller's expected case revision and
source references, then stores the draft and activity atomically. It never
creates an email job, recipient, remote ID or effect intent. No sending API exists.

Activity stores a UUIDv7 ID, actor selector, fixed action code, resource ID,
resulting revision and database timestamp. It contains no raw request payload,
credentials or arbitrary diagnostic prose. The authorized caller supplies actor
identity; the repository alone cannot establish it. Deleting application data
or activity is outside this source slice; no retention/cleanup job is added.

Queries are parameterized and schema-qualified. List results are explicitly
ordered, bounded to at most 100 rows per request and use keyset pagination with
opaque validated ID cursors; search input is 1..120 Unicode code points. Literal
search does not accept SQL patterns or expressions. Source bodies are bounded to
64 KiB UTF-8; plain text is escaped by the later renderer. Unicode validity and
control rules match the domain contract. Required-service tests exercise actual
PostgreSQL, scoped cross-record relationships, CAS races, failed activity writes,
caller rollback and duplicate draft save, with zero skips.

## Host admission gates, not source permission

[ADR 034](../adr/034-continuity-repository-boundary.md) separates repository
adoption from full host adoption. Only T-RPL-REPOSITORY-CONTRACT may complete
from this first-slice design; T-RPL-HOST-CONTRACT remains open.

The host continues to be a separate source slice. Before its implementation,
freeze exact public function signatures, validated immutable configuration,
local-only listener/origin/cookie settings, migration registry, secret custody,
request size/time bounds, operation metadata and current authority wiring.
The existing `apphost.NewLocal` is a candidate qualified local component; using
it does not qualify a newly composed application or production constructor.

Native session middleware is request admission, not proof of resource ownership.
The host must resolve the current personal workspace and recheck current active
person, realm, membership/ownership, session and assurance at the relevant
mutation/disclosure boundary, including after waits. Do not build a second
credential parser or derive authority from a configured person ID. PR2 remains
unadopted as a generic authority boundary. Any missing session-provenance/writer
serialization seam must be resolved through the existing identity/operation
lifecycle owners; an app-specific bypass is not a repair.

No live provider, external development/review service, paid subscription or agent
is necessary for this no-effect baseline. Rehearsal must prove native signup,
verification, sign-in/out, denied/revoked/foreign/CSRF cases and dependency failure.
Only the actual composed and independently reviewed boundary may be advertised.

## Exact first-slice API and schema

The first source assignment owns `examples/continuity/repository/**` and the
unallocated `examples/continuity/migrations/continuity.sql` only. No exported
constructor is added to AMOS foundation packages. Use the following application
API, with `domain` referring to the adopted continuity package:

```go
type Scope struct { InstallationID, ApplicationID, EnvironmentID, WorkspaceID uuid.UUID }
type Repository struct { /* private transaction and copied scope */ }
func New(tx *sql.Tx, scope Scope) (*Repository, error)
type Property struct {
    ID string
    Name, Area, OwnerLabel, OccupantLabel, Occupancy string
    InspectionDate string // empty or YYYY-MM-DD, validated calendar date
}
type Source struct { ID, Kind, Title, Body, SHA256 string }
type SourceSummary struct { ID, Kind, Title, SHA256 string }
type Activity struct {
    ID, ActorID, Action, ResourceID string
    Revision int64
    At time.Time
}
func (*Repository) Properties(ctx context.Context, after, query string, limit int) ([]Property, error)
func (*Repository) Property(ctx context.Context, id string) (Property, error)
func (*Repository) Cases(ctx context.Context, after string, limit int) ([]domain.Case, error)
func (*Repository) Applications(ctx context.Context, after string, limit int) ([]domain.Application, error)
func (*Repository) Procedures(ctx context.Context, after string, limit int) ([]domain.Procedure, error)
func (*Repository) Sources(ctx context.Context, after, query, kind string, limit int) ([]SourceSummary, error)
func (*Repository) Source(ctx context.Context, id string) (Source, error)
func (*Repository) Case(ctx context.Context, id string) (domain.Case, error)
func (*Repository) ChangeCase(ctx context.Context, id string, expected int64, next domain.CaseStatus, actorID string) (domain.Case, error)
func (*Repository) Application(ctx context.Context, id string) (domain.Application, error)
func (*Repository) SetChecklist(ctx context.Context, id string, expected int64, itemID string, done bool, actorID string) (domain.Application, error)
func (*Repository) Procedure(ctx context.Context, id string) (domain.Procedure, error)
func (*Repository) EditProcedure(ctx context.Context, id string, expected int64, body, actorID string) (domain.Procedure, error)
func (*Repository) SaveDraft(ctx context.Context, caseID string, expectedCase int64, body, actorID string) (domain.Draft, error)
func (*Repository) Draft(ctx context.Context, caseID string) (domain.Draft, error)
func (*Repository) Activity(ctx context.Context, after string, limit int) ([]Activity, error)
```

Sentinels are `ErrInvalid`, `ErrNotFound`, `ErrConflict`,
`ErrDecisionDisabled` and `ErrUnavailable`. Map domain errors to these exact
repository errors, retaining the human-decision distinction. Invalid parameters
are checked before SQL. Empty `after` starts a page; otherwise it is a canonical
UUIDv7 string, never SQL or arbitrary encoded state. Empty property search lists
all properties; a nonempty trimmed query has 1..120 code points. Limit is 1..100.
Property search is case-insensitive literal containment of name, area, owner or
occupant labels; escape LIKE wildcard characters if LIKE is used. Page ordering
is ascending ID and `id > after`. No total-count or complete-inventory claim is
returned from a single page. Empty pages are non-nil empty slices.

All tables use the exact scope column names `installation_id`, `application_id`,
`environment_id`, `workspace_id`, each `uuid NOT NULL`. Resource tables use `id
uuid NOT NULL` and composite primary key `(installation_id, application_id,
environment_id, workspace_id, id)`. Timestamps are `timestamptz NOT NULL DEFAULT
transaction_timestamp()`. Required text and revision constraints are enforced
in both application validation and the migration where expressible; revision
is `bigint NOT NULL CHECK (revision > 0)`.

- properties: `name`, `area`, `owner_label`, `occupant_label` text; `occupancy`
  text restricted to `occupied`, `vacant`, `unknown`; nullable `inspection_date`
  date. Name/labels have 1..200 code points, area 1..200. No authority from labels.
- sources: `kind`, `title`, `body`, `sha256` text; finite kinds above; title
  1..200, body 1..65536 UTF-8 bytes, hash exactly 64 lowercase hex characters.
  The application recomputes and compares body hash on read; mismatch is
  ErrUnavailable, never a claimed source. No HTML trust is attached.
- cases: `property_id` uuid, `title` text, `status` text, `revision` bigint,
  `source_ids` jsonb array of canonical ID strings, `updated_at` timestamp.
- applications: `property_id` uuid, `revision` bigint, `items` jsonb with exact
  fields `ID`, `Label`, `Done`, `HumanDecision` matching the domain values,
  `updated_at` timestamp. Decoding rejects unknown fields and trailing JSON.
- procedures: `title`, `body` text, `revision` bigint, `updated_at` timestamp.
- drafts: use `case_id` instead of `id` in the composite primary key; store
  `case_revision` positive bigint, `body` text, `source_ids` jsonb and
  `updated_at` timestamp. No send-status/recipient/provider columns exist.
- activity: `id`, `actor_id`, `resource_id` uuid, `action` text, `revision`
  positive bigint and `created_at` timestamp. Action is exactly `case.changed`,
  `checklist.changed`, `procedure.changed` or `draft.saved`.

Case/application property references and draft case references use composite
foreign keys containing the entire scope. Source arrays are validated under
same-scope source row locks in the owning transaction; unknown, duplicate or
foreign references cannot pass by matching only a global ID. A source record
cannot be deleted through this API. There are no role, schema-owner or provider
changes in this fragment. No fixture or business data is embedded in migration
DDL; independently authored synthetic fixtures are test-only.

Mutation methods lock the scoped current record `FOR UPDATE`, decode and validate
it, call the domain function, resolve references, and update with its original
revision in the predicate. An unexpected zero affected row after a successful
locked load is ErrConflict. They mint one activity UUIDv7 inside application
code; no externally supplied event ID is trusted. SaveDraft locks the case,
validates expectedCase against its current revision and uses the unique scoped
case key for an upsert. Repeated explicit save is another local edit/activity,
not an automatic idempotent retry. No method retries uncertain commit outcomes.

All database errors return ErrUnavailable with zero output. The caller MUST
roll back on any repository error: a failure after a successful DML statement
cannot be committed as a partial operation. Unit/required-service documentation
must repeat this obligation. Tests deliberately fail activity insertion and
prove caller rollback restores prior state, and prove successful caller commit
persists both sides. The repository cannot report a committed result itself.

Read methods validate persisted typed values before returning them; corrupted
rows are ErrUnavailable. Source/body strings are not rendered or interpreted.
The private transaction lifetime cannot escape into returned values. The
repository is not concurrency-safe for simultaneous calls on one transaction;
the owning invocation serializes its use.

Before source admission, independent review must clear these precise shapes,
error/transaction semantics and the unallocated fragment ownership. Runtime
source assignment still excludes host wiring, current authority, HTTP, seeding,
API/MCP, and migration application to an installation. Those gates stay open.

Actors and resource selectors use canonical lowercase UUIDv7 strings. Reference
locks are acquired in ascending UUID order to keep multi-source access order
deterministic. The owning transaction uses READ COMMITTED; other isolation
levels are outside this source qualification. Tests use explicit isolation.

Cases, applications and procedures have the same ascending-ID bounded keyset
pagination as properties. Each listed value receives the same persisted-value
and same-scope reference validation as its detail method. Property returns one
validated scoped register record or ErrNotFound. Sources accepts an empty kind
for both finite kinds or one exact kind, and an empty query or bounded literal
case-insensitive containment search across title and body. It returns summaries
without bodies; full text is disclosed only through Source. Source hash validation
still occurs before listing a summary, so a corrupt body cannot gain a trusted
digest by using the list path. No list silently substitutes an empty result for
database failure, and no list reports an unbounded inventory count.

## Source-grounded host authority preflight (unadopted)

Audit source: `1149112f406500e2a80c194ee7290cbd45ab2c1c`.
Scope: T-RPL-HOST-CONTRACT.1, local source list/detail disclosure only.
This appended section records observed seams and missing admission boundaries;
all preceding revision 1 bytes and its repository-only adoption are preserved.
It adopts no API, ADR, migration allocation or framework version. PR2 remains
unadopted. No runtime checks or composed journey qualification were performed
for this preflight.

### Admission matrix at the audited source

Paths below are repository-relative; function names identify the inspected
implementation rather than a proposed callable interface.

| Boundary and source | Implemented behavior and transaction owner | Missing boundary before source disclosure |
| --- | --- | --- |
| Local composition: [apphost/local.go](../../apphost/local.go), `NewLocal`, `LocalConfig.Business`, `Host.Serve`, `schemaReady`; [binding.go](../../apphost/binding.go), `Host.bind` | `NewLocal` opens development storage and a separate jobs pool, checks existing assurance/factor schema, binds the configured installation/application/environment, and passes `*storage.DB` and `*session.Service` to the business callback. `Serve` checks a loopback TCP listener and configured port. Business routes are registered with their supplied handlers. | No automatic session/workspace/authority wrapper surrounds business handlers. Binding is installation consistency, not person/resource authorization; readiness does not check continuity tables or its migration ledger. Startup opens dependencies and the host runs local mail jobs: neither is a read-only qualification probe. A continuity constructor, finite routes, immutable config, bounds and lifecycle still need adoption. |
| Browser admission: [identity/session/session.go](../../identity/session/session.go), `Service.Middleware`, `AllowsOrigin`, `csrfFromRequest` | Middleware parses the configured cookie, hashes its opaque token, and owns a three-second authentication transaction through `TxRunner.WithTx`. After successful transaction completion it builds verified credential material. For methods other than GET/HEAD/OPTIONS, it requires an allowed Origin (Referer origin fallback) and session-derived CSRF proof: one `X-CSRF-Token` value, otherwise one body-only `_csrf` in a bounded 16 KiB URL-encoded form. Repeated Origin/Referer headers fail. | Safe-method source reads do not receive an origin/CSRF check from this middleware. The future host must freeze the finite read methods and cross-origin disclosure policy; unsafe requests cannot become a read fallback. Middleware uses `r.Cookie`, not duplicate-session-cookie rejection. That compatibility change requires identity review, not another application credential parser. `workspaceRequestCheck.Valid` adds the finite form check only for the workspace UI. |
| Principal provenance: same session file, `Middleware`, `IssueForRequestTx`, `issueTx`; [identity/contracts.go](../../identity/contracts.go) | Middleware attaches trusted principal material only after authentication transaction success and unsafe-method checks. `IssueForRequestTx` stages rotation in a caller transaction and requires commit before publishing cookie/CSRF values. Principal construction stays in identity's verified-authentication plumbing. | Downstream context carries a principal snapshot, not an opaque session reference bound to this exact service instance. There is no implemented same-transaction session recheck returning a refreshed principal. `NewWithTxRunner` changes construction capability only. A person ID, context selection or configured owner ID cannot replace provenance or prove current access. |
| Session freshness: [identity/store/store.go](../../identity/store/store.go), `FindActiveSession`; [assurance.go](../../identity/store/assurance.go), `ActiveSessionAssurance` | In the caller's READ COMMITTED transaction, `FindActiveSession` locks the digest and three-part realm-matched session before sampling `clock_timestamp()`. The conditional renewal checks old idle/absolute expiry, revocation, current active person and matching security epoch. Assurance lookup follows under that session lock, samples fresh database time and downgrades expired assurance to aal1; equality is expired. | These are renewal/assurance primitives. They do not lock the person into a complete writer-compatible protocol, preserve locks after middleware commits, or recheck after later workspace/resource/policy waits. Recalling the renewing lookup is not a non-renewing disclosure-authority API. The eventual handler must receive the refreshed principal, including downgrade, rather than the earlier snapshot. |
| Workspace selection: [workspace/context/context.go](../../workspace/context/context.go), `Resolver.Middleware`, `resolve`, `requestedWorkspace`, `FromContext` | Rejects identity headers; requires a scoped person principal (machine authority is unavailable). Header/context/cookie workspace hints are selectors. `resolve` owns another `storage.DB.WithTx`: it checks active person/security epoch, active workspace and current personal ownership, or active organization membership plus role-version permissions. Unknown/foreign/suspended/unauthorized selections share denial. Selection is copied into context after transaction completion. | This separate transaction does not hold authority through repository work. Its reads have no authority row locks and do not recheck the session or assurance. The selection is a snapshot, not a current resource grant. Workspace SQL uses installation/application; environment must remain bound through trusted session/config and the repository's four-part scope. |
| Workspace persistence: [workspace/store/store.go](../../workspace/store/store.go), `New`, `FindPersonalWorkspace`, `FindWorkspace`, `FindMembership`, `ReadWorkspaceEpoch`, `ReadMembershipEpoch` | Store retains the caller's transaction. `FindPersonalWorkspace` joins active scoped person and active personal workspace using a caller-supplied person selector. Other read methods expose scoped state/epochs; `FindMembership` checks active person but returns membership state for the caller to judge. Mutators such as `UpdateMembership` lock person before workspace; `SetWorkspaceState` locks workspace. | Reads alone do not serialize authorization and do not supply session provenance. Reuse the owning workspace seam after its transaction contract is reviewed; do not copy its SQL into the continuity handler or replace current ownership with configured `ownerID` equality. Complete composition must reconcile person/workspace/membership writers, role facts and deferred owner constraints. |
| Policy and operation: [policy/contracts.go](../../policy/contracts.go), `Evaluator`, `Allowed`, `Denied`, `Unavailable`; [app/operation/operation.go](../../app/operation/operation.go), `Bind`, `NewRegistry`, `InvocationContext` | Policy defines sealed decision shapes; operation binds and snapshots metadata/codecs/resolvers/handlers. Neither supplies a current authority evaluator or exported executor. The private [invokeCallback](../../app/operation/callback_invocation.go) invalidates/drains callback SQL before result completion, but its caller still owns authority and transaction completion. | The [operation contract](operation-invocation.md) requires transaction-time state, locks and post-wait checks. Its proposed `TransactionAuthorizer` returns only `policy.Decision`, so refreshed-principal handoff remains unresolved. Native handlers need their own reviewed shared authority composition; route registration is no executor bypass. Repository `*sql.Tx` and callback `DBTX` are different capabilities; no cast, pool escape or duplicate SQL adapter is authorized here. |
| Source retrieval: [repository/repository.go](../../examples/continuity/repository/repository.go), `New`, `ready`, `args`; [list.go](../../examples/continuity/repository/list.go), `Sources`, `ids`, `listValues`; [read.go](../../examples/continuity/repository/read.go), `Source`, `source` | Repository retains one caller-owned transaction and copies four-part scope. `Sources` validates bounded literal title/body search, finite kind, cursor and limit; selects ordered scoped IDs, closes rows, then calls `Source` for each summary. `Source` checks the same scope and validates persisted content/digest. Errors return zero/nil values; unknown and foreign detail IDs are indistinguishable NotFound. | No method authorizes, commits or retries. Public `Source` uses `source(..., false)` without a row lock; reference validation's private `FOR SHARE` path is not list/detail authority. READ COMMITTED list and detail statements are not a single inventory snapshot. Scope must come from current admitted authority, with a reviewed resource/predicate strategy and freshness checks after waits. Reuse `Sources`/`Source`; no second search or SQL implementation. |
| Presentation: [ui/sourceview/sourceview.go](../../examples/continuity/ui/sourceview/sourceview.go), `RenderSources`, `RenderSource`, `RenderSourceError`, `render` | Pure functions validate supplied models and return escaped full/fragment HTML bytes, bounded to 512 KiB; validation/template failure returns nil bytes and `ErrInvalid`. Detail checks the exact body digest. Fixed not-found/unavailable views carry no diagnostics. | Renderer has no request, SQL, authority, status, headers or commit knowledge. Digest consistency proves no disclosure right. Full and fragment responses need identical admission. Host must choose safe status, no-store/security headers and method/fragment handling without treating HX headers as authority. |
| Completion: [storage/db.go](../../storage/db.go), `DB.WithTx`, plus repository and renderer above | `WithTx` begins one transaction, commits only after callback success, and rolls back unsuccessful work. Callback success alone is not commit success. Repository results and rendered bytes can be held privately until the owning call returns successfully. | There is no continuity HTTP completion adapter. Its adopted contract must suppress every source byte and success status on authority, retrieval, render, cancellation or commit failure; perform final database-time/authority checks after blocking work; and release buffered output only after successful commit. No streaming, early flush, speculative response or automatic replay of an uncertain commit is justified. |

### Failure and method admission

Existing session middleware returns 401 for absent/invalid/unavailable sessions,
403 for origin/CSRF rejection, and 503 for authentication dependency failure.
Workspace middleware returns 400 for invalid/missing selectors or prohibited
identity headers, 403 for established workspace denial, and 503 when authorization
cannot be established. These are observed mappings, not new continuity handlers.

For the future local list/detail transport, freeze these mappings before source:

| Condition | Required distinction; current implementation limit |
| --- | --- |
| Malformed cursor/query/kind/limit/detail ID | Repository `ErrInvalid`; choose the bounded transport's 400/422 mapping explicitly. No SQL or error echo from rejected values. |
| Authenticated but denied resource, unknown or foreign detail | Non-enumerating resource response, normally 404 with `SourceNotFound`; workspace/policy denial remains distinct internally. Renderer wording supplies no authorization decision. |
| Database, current-policy, recheck, render or commit failure | Unavailable (503), fixed safe wording, no source bytes and no successful empty page. Never convert missing evidence into a definite denial or allow. |
| Successful empty page | Only after actual authorized retrieval and successful transaction completion; `Sources` returns a non-nil empty slice. No total-count/inventory assertion. |
| Unsafe or unsupported method | No continuity mutation is admitted by this list/detail slice. Exact method/Allow behavior remains to be frozen; any admitted cookie mutation elsewhere retains origin and CSRF checks. GET/HEAD/OPTIONS being exempt from CSRF does not exempt them from disclosure authority. |

### SQL and migration ownership

Identity owns credentials/session SQL and principal provenance; workspace owns
workspace/member state and lifecycle SQL; the integration owner owns their
transaction-authority composition and policy semantics. Continuity owns its
existing repository SQL and pure presentation. A future native invocation owner
must hold the transaction from current authority through retrieval and successful
completion. Separate middleware transactions cannot substitute for that boundary.

[migrations.Core](../../migrations/core.go) contains sequences 1-7 and accepts
additional fragments; [NewRegistry](../../migrations/registry.go) copies a
contiguous, unique sequence. The reference app already occupies sequence 8.
[cmd/amos/reconcile.go](../../cmd/amos/reconcile.go), `billingMigrationRegistry`,
and [generated application templates](../../internal/scaffold/application/templates.go)
compose existing fragments through 16. The operation contract's sequence 17 is
only a candidate, not an available continuity allocation.

[continuity.sql](../../examples/continuity/migrations/continuity.sql) remains an
unallocated application fragment with no exported registry constructor.
[storage.Migrate](../../storage/migrate.go) serializes migration transactions,
checks exact ledger-prefix IDs/checksums and commits each SQL fragment and ledger
entry together. Only the integrator may compare the target application's exact
immutable history and allocate a new isolated composition. Do not replace the
reference fragment at 8, assume 17 is free, reorder existing entries, or apply DDL
from a request/host startup callback. Fresh fixture fragment provisioning does
not qualify an existing installation upgrade. This audit allocates nothing.

### Minimum next contract work and source gate

The next bounded work is T-RPL-HOST-CONTRACT.2's **design**, following independent
review of this preflight; it is not a source-ready host assignment. Keep the
following interfaces conceptual until the existing lifecycle owners agree exact
signatures, semantics and an independently reviewed amendment:

1. **Trusted session provenance and current-principal handoff.** Identity alone
   mints an opaque, immutable reference after successful authentication commit
   and request admission, bound to the exact service instance. A caller-owned
   READ COMMITTED transaction can recheck it without accepting raw credentials,
   caller session IDs, caller time or a fabricated principal, and receive the
   current principal including assurance downgrade. Resolve duplicate-cookie
   semantics explicitly. No public method name/signature is adopted here.
2. **Current workspace/resource authority.** A shared owner supplies current
   realm/person/session/workspace/ownership or membership/permission facts and
   the finite read requirements in that same transaction, with a complete
   writer-compatible row/predicate protocol. Recheck database time, expiry and
   current decisions after later resource/policy waits and before completion.
   Define how refreshed principal and selection reach the consumer. A policy
   decision alone cannot silently refresh either value.
3. **Finite local transport completion and migration composition.** Adopt exact
   config, methods/routes, request/output/time limits, safe error/header mapping,
   full/fragment buffering and commit ownership, and an isolated migration
   registry allocation. Consume existing repository/renderer APIs unchanged.
   No registered operation is exposed until its shared executor gates close.

The [session preflight](../planning/session-recheck-preflight.md) remains open.
The implemented `FindActiveSession`/`ActiveSessionAssurance` and `ConsumeChallenge`
post-lock corrections are primitive-only evidence. `ConsumeChallenge` checks
READ COMMITTED, locks the exact ID/purpose/digest row, samples database time,
and conditionally consumes with that instant; it neither qualifies subsequent
producer waits nor establishes person/session authority.

A concrete unresolved producer prerequisite is
[identity/email/email.go](../../identity/email/email.go), `Service.Confirm`:
its separate inline challenge UPDATE still tests expiry and sets `consumed_at`
with `transaction_timestamp()` before later email/person work. It does not call
`ConsumeChallenge`. The next narrowly scoped producer design must address that
actual path's post-wait freshness and transaction graph under its existing owner;
this preflight does not authorize its repair. Complete graphs also remain open
for rotation (`issueTx` revokes an old session before new-session insertion),
recovery's bulk revocation, magic-link proof/policy timing, MFA and federation.
Include cross-person old cookies, foreign-key/unique-index waits, deferred
workspace owner triggers and callback/predicate edges. Preserve federation's
existing final flow fence; no whole-producer correctness follows from its presence.
Do not cap a revocation set or impose a new lock order only inside a leaf helper.

Ownership remains external to this document assignment: T2.8 retains shared
operation/current-authority integration; existing identity-flow claims including
T3.5, T3.9 and T3.15 and workspace UI claim T6.4 are not delegated by this
preflight. T16.1 release evidence ownership is also retained. Exact remote claim
identities and the delegated preflight claim are recorded in the private handoff;
no claim was acquired, released or inferred expired. Before dependent edits,
recheck exact owner custody and obtain explicit path delegation. This author
owns only this appended section, not identity, policy, workspace, migrations,
plan acceptance or shared wiring.

Before any dependent source admission, independently review the exact authority
amendment, complete writer graph, refreshed handoff and migration allocation.
Prescribe real bounded two-connection schedules for revocation/epoch changes,
idle/absolute/assurance expiry across observed waits, workspace suspension and
foreign resources, cancellation, rollback and failed commit. Actual HTTP tests
must prove no full/fragment bytes escape denied/unavailable/failed completion,
unsafe origin/CSRF rejection and no-store behavior; composed browser checks
remain separate. Primitive fixtures and pure-renderer checks cannot satisfy
those gates. T-RPL-SEARCH.6 still gates T-RPL-WEB.1; T-RPL-WEB.6 and
T-RPL-HOST-CONTRACT.6 still gate T-RPL-HOST.1. Unresolved authority blocks
composition; no task acceptance or replacement readiness follows from this audit.


The [concrete integration path](../planning/replacement-integration-path.md)
orders the remaining shared authority, retrieval, HTTP completion, baseline
business host and rehearsal work over the existing task inventory. Its next
artifact is an exact authority contract, not another source audit. This link
adds no API adoption, task acceptance or source permission.
