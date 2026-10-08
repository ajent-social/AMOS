# Continuity source views, revision 1

Status: proposed; independent exact-head review and guarded adoption precede
source. This is a pure presentation prerequisite, not an authenticated endpoint.

## Existing retrieval and missing authority

Reuse `repository.Sources` and `repository.Source` from the adopted
[repository contract](continuity-host.md). They already implement bounded literal
search, kind filtering, four-part scope, keyset pagination and body-digest checks.
Do not create a second SQL/search engine under `examples/continuity/search`.
A later authenticated caller owns the transaction, current authority, retrieval,
commit outcome, HTTP status and no-store/security headers. Scope identifiers,
source digests and rendering success confer no permission to disclose content.

Session middleware, workspace selection and the unadopted owner-route proposal
are not a complete current resource-authority boundary. T-RPL-SEARCH, T-RPL-WEB,
T-RPL-HOST-CONTRACT and T-RPL-HOST retain their complete composed acceptance.
Their denied/foreign/revoked/CSRF and actual-browser gates remain open.

## Exact application-owned API

The source assignment owns only `examples/continuity/ui/sourceview/**`.
It may import the existing repository value types and standard library; no shared
module, framework renderer, database, handler, migration or executable changes.

```go
type SourceList struct {
    Query, Kind, After string
    Limit int
    Sources []repository.SourceSummary
}
type SourceError string
const (
    SourceNotFound SourceError = "not_found"
    SourceUnavailable SourceError = "unavailable"
)
var ErrInvalid error
func RenderSources(model SourceList, fragment bool) ([]byte, error)
func RenderSource(source repository.Source, fragment bool) ([]byte, error)
func RenderSourceError(kind SourceError, fragment bool) ([]byte, error)
```

All functions are pure: no HTTP request/writer, context, credentials, I/O, clock,
SQL, permission decision or effects. Return newly owned bytes on success and nil
bytes plus exact ErrInvalid on invalid input. Inputs are never changed. No partial
output escapes template failure. Templates are immutable and calls are safe in
parallel. A package-private immutable parsed template is permitted; mutable
per-request package state is not. No caller-supplied template, HTML, URL or script
escape hatch exists.

## Validation and pagination

Reject excessive raw input before scanning or copying: IDs at most36 bytes,
Kind at most14 bytes, Query at most480 bytes, titles at most800 bytes, digest
at most64 bytes and detail body at most65536 bytes. Already-normalized repository
values fit these limits; direct padded inputs may be rejected. Bound every result
to512 KiB; overflow returns nil and ErrInvalid.

Validate the entire model before rendering. IDs use canonical lowercase RFC4122
UUIDv7. Query is empty or trimmed 1..120 code points, valid UTF-8, no controls;
controls must be rejected before trimming. Kind is empty, `correspondence` or
`document`. After is empty or a valid ID. Limit is 1..100. Sources length is at
most Limit. Summaries have distinct strictly ascending IDs, each greater than
After when nonempty. Kind must be one of the two finite kinds and agree with a
nonempty filter. Titles are trimmed 1..200 code points, valid UTF-8, no controls.
Summary SHA256 is exactly 64 lowercase hex characters. No body is present in a
summary: the renderer cannot establish hash consistency or authenticity from it.

Detail has a valid ID, finite kind and title above. Body is 1..65536 UTF-8 bytes,
nonempty after trimming, with ASCII controls other than tab/newline rejected.
SHA256 must equal the lowercase SHA-256 of the original untrimmed body bytes.
Render those original body bytes as escaped text; do not normalize, summarize,
parse Markdown, auto-link URLs or execute source instructions. Hash equality
means byte consistency only, not authenticity, truth or authority.

The list heading is Sources. Show the normalized query and selected kind in an
ordinary GET search form with labelled input `q` (maxlength120) and select `kind`,
fixed action `/continuity/sources`. Preserve Limit in a hidden `limit` field.
Searching starts a new page, so the form must not carry the old After cursor.
Show each supplied title, finite kind and supplied digest; use only a fixed local
link `/continuity/sources/<canonical ID>`. No titles/body/other strings form paths.

If len(Sources) == Limit, offer a link labelled `Try next page` to the fixed list
path with `after` set to the final returned ID and the normalized `q`, `kind` and
`limit` preserved using proper URL query encoding. Otherwise omit it. This is an
attempt to continue, not proof that more records exist. Empty results say
`No sources in this page.` Never claim complete inventory or a global count.
Always describe the list as a supplied page whose retrieval scope is caller-owned.

## Rendering and fixed error states

Use `html/template` with no `template.HTML`, `template.URL`, `template.JS` or other
trusted-content conversion. All source/query/title/body values are escaped text.
Fixed plain application wording describes correspondence/documents and digests;
source text cannot become a success, permission, command or policy message.
Detail shows title, kind, digest and original body in a wrapping `pre` element.
No edit, send, approval, download or external-navigation affordance is added.
The only navigation is back to `/continuity/sources` or the finite detail links.

The full document uses lang=en, UTF-8, viewport metadata, a descriptive escaped
title, a skip link, `main id="main-content"` and the existing `/assets/base.css`.
Keep this semantic component within the existing UI conventions; no replacement
visual system or new design direction is introduced. A small fixed source-view
style may wrap long text/digests and prevent overflow; it contains no input data.
Every full and fragment render has exactly one root source-view section with
`id="continuity-source-content"`. Full embeds it in main; fragment returns only
that same section, without html/head/body/main or asset tags. Later transport must
select appropriate HTMX target/swap semantics; these functions do not inspect
HX headers or authorize a response. Content remains usable without JavaScript.

SourceNotFound says `Source not found.` without distinguishing missing from denied.
SourceUnavailable says `Sources are temporarily unavailable.` and never substitutes
an empty list or successful detail. Error states contain only fixed application
text and the fixed back link; unknown error values fail ErrInvalid. The future
handler maps these to appropriate HTTP status; rendered bytes have no status.

## Required evidence and preserved gates

Test full and fragment list/detail/error output, empty and bounded maximum pages,
query encoding and cursor preservation/reset, malformed IDs/ordering/filter/digest,
UTF-8/control/size limits, exact original body, escaped executable-looking titles,
queries and bodies, no external or injected URLs, fresh output and input isolation,
and concurrent rendering. A meaningful mutation disabling escaping or accepting
a wrong detail hash must fail the intended test; exact restoration must pass.
Normal/race/vet/pinned lint and different exact-head review apply. Test structural
HTML and default form/navigation behavior. Use an existing local browser on a
new static synthetic rendered artifact, JavaScript disabled, to inspect escaping,
keyboard focus and long content at a narrow viewport. No server, credentials or
service are needed; this is presentation evidence only. Actual composed browser
journeys and current-authority qualification remain T-RPL-WEB/rehearsal work.

Six additive T-RPL-SOURCE-VIEW rows record this presentation component. Existing
search/web/host tasks and all original task records remain intact. This contract
and a tested renderer cannot accept RB-6/RB-8/RB-9, a live endpoint, persistent
retrieval, a full product task, provider behavior or replacement readiness.
