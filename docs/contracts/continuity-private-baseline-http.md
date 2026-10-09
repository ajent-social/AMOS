# Private continuity baseline HTTP reads

Status: finite B1 proposal; independent exact-head design review precedes source.
This supplies an unexported `(*readCore).baselineHandler() http.Handler` over the
reviewed private baseline reader. No public host, listener or mutation endpoint
is enabled. All complete native-writer, executor and host gates remain required.

## Closed routes and request bounds

Allow only GET and HEAD for these exact routes:

| Route | Query fields |
| --- | --- |
| `/continuity/properties` | q, after, limit |
| `/continuity/properties/{id}` | none |
| `/continuity/cases` | after, limit |
| `/continuity/cases/{id}` | none |
| `/continuity/applications` | after, limit |
| `/continuity/applications/{id}` | none |
| `/continuity/procedures` | after, limit |
| `/continuity/procedures/{id}` | none |
| `/continuity/activity` | after, limit |
| `/continuity/guide` | topic, case_id, limit subject to the rules below |

IDs and cursors are canonical lowercase RFC4122 UUIDv7 values. List limits are
canonical decimal 1..100, default25. Property search has at most480 raw bytes and
120 trimmed Unicode code points, with valid UTF-8 and no controls before trimming;
empty or whitespace-only search normalizes to no filter. Other pages accept no
search field, including an empty one. Reject unknown or duplicate keys and malformed
percent encoding. Details prohibit every query string, including a bare trailing
question mark. A trailing slash, extra path segment or malformed detail ID is not
normalized into another resource. Missing routes return404; invalid selectors400;
unsupported methods405 with `Allow: GET, HEAD`.

Guide defaults to attention with a limit of25. Attention and handover accept a
canonical limit1..100 and an absent or empty case_id. Explain-case and owner-draft
require a canonical case_id and reject an explicitly supplied limit. Spending-
authority accepts absent/empty case_id and rejects an explicitly supplied limit.
The topic is one exact existing guide value; an explicitly empty topic is invalid.
An empty case selector can be emitted by the ordinary guide form; it never selects
a case or establishes authority. The current personal workspace is always selected
through the native resolver; there is no client workspace, actor or principal field.

Use the existing source adapter's transport bounds: path at most128 bytes, raw
query at most2048, no RawPath alias, absolute URL, URL host/user/fragment, declared
body or transfer encoding. Bound raw values before semantic scans. No request body
is read. The incoming context is bounded to at most five seconds without extending
an earlier deadline. Exact single `HX-Request: true` plus single
`HX-Target: continuity-baseline-content` selects a fragment; every other header
combination selects the complete document, as in the existing source adapter.

## Same-instance admission and buffered completion

Capture the entire existing private session middleware chain in the bounded
collector. After native admission, obtain the CSRF form token only for case,
application and procedure detail using this exact session service's
`CSRFToken(*http.Request)`. A missing token fails unavailable with no provisional
page. Never read a token from a query, header, body or custom cookie parser.
Pass it only as presentation input to the private baseline reader. Other pages
receive no token. This GET does not verify a POST or authorize a mutation.

Call the existing private baseline reader with the admitted original request
context and validated selectors. The response may be marked committed only after
that call returns success. Repository, rendering, final current-state comparison,
commit and cancellation gates stay in the reader. HEAD performs the same work and
headers as GET but publishes no body. Missing and established-denied resources
use the existing non-enumerating404 mapping; a missing native session may retain
the middleware's401. Invalid input400 and unavailable503 remain distinct.

Reuse the existing collector's enforced512KiB body bound, final8KiB/32-value header
publication budget, panic/cancellation handling, no streaming/unwrap surface and
commit-marker requirement. Do not duplicate its publication rules. A private
closed presentation enum may select source versus baseline error presentation;
it accepts no caller template, renderer, HTML, URL, target or policy callback.
Keep the existing source wrapper and behavior unchanged. Unknown presentation
values fail unavailable and cannot disclose a collected success body.

Every non-success discards provisional bytes and native diagnostics. Baseline
errors use fixed escaped wording, finite code, a fresh server UUID request ID,
the baseline fragment section and a fixed property-register recovery link. Source
errors retain their source section, wording and recovery link. The presentation
choice cannot change HTTP status, authority or completion. Strip cookies,
redirects, cache validators and incoming correlation IDs. Preserve the existing
no-store, security, CSP and Vary headers on full, fragment, error and HEAD replies.

## Verification and limits

Pure tests cover every route/query combination, raw/decoded bounds, method/body
rejection, exact fragment fallback, CSRF input exclusion, both error presentations,
fresh correlation IDs, HEAD, panic, publication budgets, cancellation and missing
commit markers. Genuine negatives must expose an uncommitted success or wrong
fragment/error section and pass after exact restoration. The shared collector
refactor requires focused existing source HTTP regression checks.

Required-service tests exercise the actual native middleware, same-instance CSRF
token retrieval, each baseline GET/HEAD/list/detail/guide page and scoped denial,
corruption, cancellation and failed completion cases actually available. Missing
services fail visibly. Any unavailable schedule remains an explicit open gate;
fixtures and pure collector tests cannot replace failed-commit or complete native
journey evidence. All runtime operation remains with the independently qualified
reviewer after a fresh exact fixture grant; source authors have no such grant.

The integrator owns the new baseline HTTP source/tests, the minimum private
collector/presentation refactor in source_http.go and host README. No mutation
routes, shared schemas, migration registration, executable wiring or public
constructors are part of this component. Real POSTs still require native origin/
CSRF checks and the full shared registered-operation executor. Static forms and
these private reads do not qualify RB1–RB10, providers or full host acceptance.
