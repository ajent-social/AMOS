# Private source-read composition

This package contains unexported composition and source HTTP helpers. It exposes
no public host constructor, HTTP listener, route registration, schema installer,
provider capability or business mutation. The reviewed private-read contract
controls its scope; full writer/provider and public host admission remain open.

The constructor owns one runtime connection pool, root, native session service
and workspace resolver. No caller can supply an assembled authority capability or
transaction through its API. The private source method requires that exact
session's admission, resolves the current personal workspace, and calls the
existing repository and renderer. After rendering it drains deferred work,
resolves the pinned workspace again and performs the last session recheck.
Only pure policy/equality checks follow that instant. Bytes leave only after
successful transaction completion and the root cancellation check.

Required-service test source uses synthetic account/session setup and actual
native middleware to exercise personal list/detail and literal search. It does
not qualify a full signup/sign-in-to-business journey, actual commit-loss
schedules, organization policy, providers or RB1-RB10. The fixture must include
the independently qualified identity/workspace/W1 and continuity fragments in
one isolated runtime database; missing prerequisites fail visibly.

The private source HTTP adapter accepts only the fixed list/detail GET and HEAD
routes. It validates bounded selectors before native middleware admission and
captures all middleware output in a private collector. Only successfully committed
source bytes may become a 200 response; failures use fixed escaped messages and
fresh server correlation IDs. HEAD performs the same work but omits the body.
Fragment output requires the exact declared header pair; all other metadata uses
a full document. Every response is no-store, and no cookies or redirects escape.

The scoped transport tests cover parsing, publication budgets, error replacement,
cancellation and panic discard. Test-only supplied responses prove transport
mechanics, not actual authentication or failed database completion. Real native
HTTP retrieval, failure schedules and the full browser journey remain unqualified
until independently exercised with a separately authorized fixture. This adapter
does not register a public route or satisfy full host admission.

The private baseline reader uses a closed page selector for the existing property,
case, application, procedure, activity and guide views. It shares the source
reader's exact admission, workspace and final completion path. Supplied form tokens
are presentation data; they never grant admission or authorize a mutation. Guide
selection preserves existing case/reference validation and rejects more than 100
distinct references before retaining its source-body collection. All work remains
inside the original bounded transaction. A missing saved draft is optional; other
draft failures suppress the page. No business mutation or public route is enabled.

The private baseline HTTP adapter binds only the reviewed property, case,
application, procedure, activity and finite guide GET/HEAD selectors. It parses
bounded route/query shapes before admission and obtains native form tokens from
that same session service only afterward. No POST route is implemented. The
existing collector enforces one publication boundary for both source and baseline
responses; a closed presentation selector chooses only their fixed error pages.
Unknown presentation values cannot release a collected success response.

Baseline transport tests cover selectors, exact fragment fallback, HEAD, fixed
error sections, correlation and buffered completion. Required-service test source
reuses the same synthetic scoped setup with native middleware and checks stored
reads, form tokens, stale drafts and no-output failures. Pure tests and authored
runtime tests are not evidence of actual required-service execution, failed
commit, working HTMX, form submissions or the complete native/browser journey.
