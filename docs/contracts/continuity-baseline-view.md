# Continuity baseline presentation

Status: finite B1 proposal; independent exact-head design review precedes source.
This is application presentation over caller-supplied values. It does not admit
an HTTP host, registered mutation, database, principal or external effect.
The complete RB1-RB9 journey and RB10 rehearsal retain their existing gates.

## Owned surface and reuse

The source assignment owns only `examples/continuity/ui/baselineview/**`.
Reuse the existing domain, repository and deterministic guide value types. The
existing `sourceview` package remains the RB6 source list/detail renderer. Do not
copy its SQL, replace its files or add a competing source model. Shared identity
screens and credential handling remain with native identity owners.

```go
type PageCursor struct { After string; Limit int }
type PropertyList struct {
    PageCursor
    Query string
    Properties []repository.Property
}
type CaseList struct { PageCursor; Cases []domain.Case }
type ApplicationList struct { PageCursor; Applications []domain.Application }
type ProcedureList struct { PageCursor; Procedures []domain.Procedure }
type ActivityList struct { PageCursor; Activity []repository.Activity }
type CasePage struct {
    Case domain.Case
    Draft *domain.Draft
    CSRFToken string
}
type ApplicationPage struct { Application domain.Application; CSRFToken string }
type ProcedurePage struct { Procedure domain.Procedure; CSRFToken string }
type ViewError string // invalid, not_found, conflict, unavailable, unsupported
var ErrInvalid error
func RenderProperties(PropertyList, bool) ([]byte, error)
func RenderProperty(repository.Property, bool) ([]byte, error)
func RenderCases(CaseList, bool) ([]byte, error)
func RenderCase(CasePage, bool) ([]byte, error)
func RenderApplications(ApplicationList, bool) ([]byte, error)
func RenderApplication(ApplicationPage, bool) ([]byte, error)
func RenderProcedures(ProcedureList, bool) ([]byte, error)
func RenderProcedure(ProcedurePage, bool) ([]byte, error)
func RenderActivity(ActivityList, bool) ([]byte, error)
func RenderGuide(guide.Request, guide.Selection, bool) ([]byte, error)
func RenderError(ViewError, bool) ([]byte, error)
```

The final bool selects a fragment. Functions perform no I/O, HTTP handling,
credential parsing, clock sampling, SQL, authorization or mutation. They return
newly owned bytes or nil plus exact ErrInvalid. Invalid models never produce
partial output. Inputs and nested slices/pointers are not mutated or retained.
Templates are immutable and concurrent calls use separate bounded buffers.

RenderGuide calls the existing `guide.Answer` and renders its validated result;
it does not implement a second briefing algorithm or invent source evidence.
Unsupported or missing guide input fails ErrInvalid; a later transport selects
fixed unsupported/unavailable presentation without describing it as a model reply.

## Bounds and validation

Reject raw excessive fields before copying or scanning: IDs at most36 bytes,
titles/labels/names/areas at most800, query at most480, procedure body at most24000,
draft body at most16000, token exactly43, and each ID/item array at most32 entries.
List lengths are at most100 and no greater than Limit, which is1..100. After is
empty or canonical UUIDv7. Rows have distinct strictly ascending IDs greater than
After; activity uses its existing ID order. Empty lists remain ordinary empty
pages, never proof of complete inventory. All output is capped at512 KiB during
template execution; overflow discards the whole output.

Use canonical lowercase RFC4122 UUIDv7 IDs, positive revisions, valid UTF-8 and
the existing domain field bounds and finite enums. Text labels reject controls
before trimming; body text permits tab/newline but not other ASCII controls.
Validate all supplied record fields even when the list displays only summaries.
Support the last positive revision as a readable value; do not run a mutation to
validate a view. Source IDs are distinct canonical references, never authority.
Property occupancy is occupied/vacant/unknown; inspection date is empty or a
valid calendar YYYY-MM-DD. Checklist items preserve exactly one human-decision
item, always Done=false; malformed supplied decisions fail rendering.

CSRFToken is a supplied native-session form token, never a session credential.
Validate canonical unpadded base64url encoding of32 bytes. The host must obtain
it from the native session API and own current authority, origin and CSRF checks;
the renderer neither creates nor verifies proof. No cookie parser is added.

A supplied Draft must have the displayed case ID, positive CaseRevision no newer
than the current Case.Revision, valid bounded body and source IDs. Show its saved
revision and references separately. A stale saved draft displays a fixed warning
that the case changed. It never becomes current automatically; the save form
carries the current case revision and the user must explicitly submit it. This
presentation does not resolve source permissions or authorize rebasing a draft.

Activity validates UUIDs, positive revision, nonzero timestamp and only the four
existing action codes. Display its timestamp as UTC; no app clock determines
state or freshness. Unknown actions fail closed rather than rendering raw codes
as trusted narration. The supplied actor ID is a label, not actor authority.

## Routes, forms and observable behavior

All URLs derive only from fixed route prefixes and validated IDs/query values.
Use these later-host binding targets; a rendered form is not a working endpoint:

| View | Ordinary route or form |
| --- | --- |
| Property register/detail | GET /continuity/properties and /continuity/properties/{id}; register search q, after, limit |
| Case register/detail | GET /continuity/cases and /continuity/cases/{id}; POST detail/status with expected and status |
| Unsent draft | POST /continuity/cases/{id}/draft with expected and body; show existing saved draft and source links on case detail |
| Application register/checklist | GET /continuity/applications and /continuity/applications/{id}; POST detail/checklist with expected, item_id and done |
| Procedure register/editor | GET /continuity/procedures and /continuity/procedures/{id}; POST detail with expected and body |
| Activity | GET /continuity/activity with after and limit |
| Deterministic guide | GET /continuity/guide with the existing finite topic and optional case selector |

Every POST form contains one hidden `_csrf` and one positive `expected` revision.
No caller-supplied action URL, method, field name, HTML or JavaScript is accepted.
Case status choices are exactly the five domain values with human-readable labels;
state changes are described as operator records, not approval or verified outcome.
Save draft says explicitly Unsent and offers no send/recipient/provider control.
Checklist supporting items have separate ordinary forms and explicit boolean
values; the final human decision is visibly disabled with explanatory text and
has no submit control or mutable named input. No score/rank/accept/reject exists.
Procedure text is escaped in a labelled textarea and cannot alter executable rules.

All navigation works without JavaScript. Ordinary links/forms are the fallback;
progressive HTMX attributes may target only the declared fixed fragment section
and retain the same methods, fields, CSRF and server policy. No external CDN or
script is loaded. The eventual transport must qualify actual HTMX loading and
full/error fragment behavior before claiming enhancement works.

Lists use next-page links only when the supplied length equals Limit, labelled
Try next page, preserving normalized filters. Search clears the previous cursor.
Property detail links to the case/application registers without pretending a
relationship or filtered result was retrieved. Cases link to their property and
supplied source references; all source links use existing sourceview paths.
Guide responses distinguish supplied records from unknown authority, preserve
source references and describe draft output as unsent. No invented facts appear.

## Rendering, errors and accessibility

Use html/template without trusted HTML/URL/JS conversions. Plain record text,
including executable-looking source content, remains escaped data. Full documents
retain the existing base.css, lang=en, UTF-8, viewport, descriptive title, skip
link and main landmark. Each full/fragment result contains exactly one root section
`continuity-baseline-content`; fragments contain no document/main/asset wrapper.
Fixed navigation reaches properties, cases, applications, procedures, sources,
guide and activity. No new visual system or private design asset is introduced.

Use semantic headings, labelled fields, visible native focus, table headers or
list semantics, wrapping text/digests and responsive forms that do not overflow
at390px or200% text zoom. Fixed empty/error/conflict states include a safe local
recovery link. Conflict explains that the record changed and requires reloading;
it never silently overwrites current state or claims the user's input was saved.
No database/provider diagnostics or arbitrary error strings are rendered.

The error renderer supplies presentation only. The later transport owns HTTP
status, fresh server correlation ID, no-store/security headers, safe retained
input and final committed output. It must not publish a provisional success or
reuse a success page for an unavailable dependency.

## Verification and integration limits

Required pure tests cover every page/list/form, full and fragment output, invalid
UTF-8/IDs/enums/revisions/controls/bounds, alias isolation, escaped executable text,
pagination, stale draft warning, fixed failures and final-decision form absence.
Normal/race/vet/pinned lint and independent exact-head review apply. Genuine
negative/restored tests must catch an escaping or human-decision form regression.
Static synthetic rendered artifacts may be checked in an existing browser with
JavaScript disabled for keyboard, narrow width and text zoom. They are presentation
evidence, not actual stored changes or authenticated browser journeys.

The host must still bind qualified registered operations through the shared
executor, preserve atomic revision/activity/draft behavior, and exercise real
native identity, current policy, CSRF, conflicts, denied/foreign/dependency and
restart paths. This component cannot complete WEB, HOST, RB1-RB10, SaaS, billing,
generator, provider, portability, release or replacement acceptance.
