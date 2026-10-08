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
