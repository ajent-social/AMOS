# Private continuity baseline read composition

Status: finite B1 proposal; independent exact-head design review precedes source.
This extends the existing private source reader to the supplied baseline views.
It enables no public host, listener, mutation, executor or provider. The complete
writer graph, registered-operation and RB journey gates remain mandatory.

## Private surface and ownership

The integrator owns `examples/continuity/host/baseline_read.go`, the necessary
private dispatch refactor in `read.go`, tests and host README. Repository, domain,
guide, sourceview and baselineview APIs are reused. No SQL, authority lookup or
credential parser is copied into the application. Shared operation execution,
identity producers, migrations and public construction remain separate gates.

```go
type baselineQuery struct {
    WorkspaceID uuid.UUID // zero selects the current personal workspace
    Page baselinePage // closed private enum below
    ID, After, Query string
    Limit int
    Topic guide.Topic
    CaseID string
    CSRFToken string // supplied presentation token, not admission or authority
    Fragment bool
}
func (*readCore) baseline(context.Context, baselineQuery) ([]byte, error)
```

The only page values are property list/detail, case list/detail, application
list/detail, procedure list/detail, activity and guide. There is no arbitrary
route, table, query function, renderer, principal, transaction, policy or callback
argument. All new methods and types are unexported. The exact already-owned root,
session service and workspace resolver remain the sole construction chain.

Lists accept only their declared cursor and limit, with Query supported only for
properties. Details require a canonical UUIDv7 ID and prohibit list/guide fields.
Case, application and procedure detail additionally require the supplied native
form token. All other pages reject a token. Guide requires one of the existing
five topics, the topic-specific optional CaseID, and a bounded Limit for attention
and handover selection; other guide topics reject cursor/query/limit. Unsupported
combinations fail the fixed invalid class before transaction work. All raw strings
are length checked before scanning, using the adopted view bounds. Query control,
UTF-8 and trimmed rune rules and canonical cursor/ID rules remain unchanged.

CSRFToken is presentation data only. The later native HTTP adapter must obtain it
from this same session instance's `CSRFToken(*http.Request)` after middleware
admission. Possessing or passing a token never replaces the private admission
required by `RecheckCurrentTx`. This read method does not validate a POST or claim
that a rendered form is an enabled endpoint.

## One authority and completion path

Retain the exact private source-read contract's transaction ordering. To avoid two
authority implementations, refactor its existing body into a private method whose
input selects either the existing source query or a validated baseline query,
never both. The branch is closed native code; no caller-supplied callback is added.
Existing `source` and new `baseline` enter that same method. Source rendering and
all of its selectors, errors and final completion behavior stay unchanged.

Inside `Root.Read`, recheck this instance's privately admitted session, resolve the
current personal workspace and apply the existing personal-read predicate. Derive
repository Scope solely from the refreshed principal and selection. The selected
branch calls the existing scoped repository methods and pure renderer. It cannot
mutate domain state or open another transaction. Continue to use the legacy
caller-transaction repository constructor on this read path; adopting the separate
DBTX adapter does not manufacture an operation invocation or callback lifetime.

Property, case, application and procedure lists/details and activity use their
existing typed repository methods and corresponding baselineview functions.
Case detail reads the case and then its saved draft. Only repository.ErrNotFound
for that draft means no supplied draft; every other draft failure suppresses the
whole page. The renderer preserves stale-draft revision and warning semantics.

Guide uses the existing deterministic `guide.Answer` through `RenderGuide`.
For explain-case and owner-draft, read exactly the selected case. For attention and
handover, read one supplied case page of at most Limit (1..100), with no cursor;
describe the selection as bounded, not all records. Spending-authority supplies an
empty selection and retains its explicit unknown-authority response. Collect the
selected cases' distinct source IDs, rejecting more than100 before building the
guide's retained source-body collection. Existing Repository.Case/Cases already
validate each case's references through Source; their bounded validation may read
up to100 cases times32 references before this aggregate check. Preserve that
validation rather than bypassing it or claiming that no body was read. All work
remains under the same original Root.Read deadline. After the aggregate check,
fetch each distinct source through Repository.Source in canonical ID order,
retaining at most100 bodies and preserving current scope and digest checks.
Do not truncate references,
duplicate search SQL, infer missing sources or treat source text as instructions.
An oversized selection is unavailable; it does not return an incomplete briefing.

Every rendered byte remains private memory. After rendering, drain deferred work
with `SET CONSTRAINTS ALL IMMEDIATE`, re-resolve the pinned workspace and perform
the last session recheck. Compare the complete final selection and apply the
personal-read predicate to the last refreshed principal. No SQL, renderer or
callback follows this final DB instant. Only a successful Root.Read commit and its
cancellation fence release bytes. All errors return nil bytes with the existing
fixed invalid/not-found/denied/unavailable classifications; no diagnostics escape.

## Verification and limits

Source review must trace every page branch, full field-combination validation,
bounded guide collection, optional draft handling and the single unchanged final
authority path. Pure selector tests cover all page combinations and invalid bounds.
Required-service tests use actual private middleware admission and scoped records
to exercise every list/detail, guide selection, saved and stale drafts, missing and
foreign records, source corruption, cancellation and dependency failure. Missing
services fail visibly. The source author has no fixture operation grant.

The read refactor justifies a focused regression run of the existing ordinary
source HTTP suite after integration; it does not justify rerunning unchanged
identity suites. Any platform-blocked operation remains excluded and unverified,
including renamed or equivalent probes. Genuine negative/restored checks must
detect a missing draft error or incomplete guide selection without claiming they
qualify failed commit or the complete authority/writer graph.

Normal/race/vet/pinned lint and independent exact-head review apply. Later HTTP
composition still owns finite method/query/body parsing, same-instance CSRF token
retrieval, security/no-store headers, buffered response publication and actual
browser behavior. Registered mutations still require the full shared executor,
current policy, resource locks, replay/capacity/audit/effect atomicity and independent
real qualification. No baseline form, private read or fixture closes these gates.
