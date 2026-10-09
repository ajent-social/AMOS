# Authenticated replacement component checkpoint

This is an in-progress record within the coherent B1 candidate. It is not a
separate evidence delivery, task acceptance, merged-source claim or host
admission. The complete sixteen-scope batch and its full gate matrix remain in
[the batch plan](../planning/authenticated-replacement-batch.md). Original task
inventory 1,501, current inventory 1,598 and accepted product tasks 59 are unchanged.
Historical accepted components keep their original boundaries.

## Independently reviewed component evidence

Counts below are test pass events from the named finite selections, including
parent tests when the runner emits them. Each passing selection had zero failed
or skipped test events. A component's original head is not a claim that every
later candidate change was covered by its checks.

| Component and exact source boundary | Independent evidence | Limit |
| --- | --- | --- |
| Bounded password computation at `26bb78a` | Normal/race/restored 14 each; three genuine lifecycle negatives; vet and pinned lint | Real protection guard and bounded computation, not a native sign-in producer |
| Ordering root at `42eb3a7` | Normal/race/restored 44 each; six genuine protocol/deadline/completion negatives; vet and pinned lint | Ordering and transaction lifetime, not credential authority or the complete writer graph |
| Copied sealed inventory, `0119dce` integrated at `893b7c9` | Additive normal/race/restored 4 each; aliased-copy negative | Only the new inventory delta; earlier root coverage retains its earlier source boundary |
| Same-transaction workspace resolver at `d03529c` | Normal/race/restored 100 each; four state/epoch/copy/lock negatives; vet and pinned lint | Refreshed principal input remains the private session/composition owner's obligation; operation policy is not supplied by a resolver |
| Pool-free delivery participants at `b063605` | Normal/race/restored 77 each; four purpose/reference/failure/order negatives; vet and pinned lint | Material/job persistence only; no email dispatch or live provider qualification |
| Closed identity evidence corrected at `e8d09da` | Normal/race/restored 23 each; four one-use/action/expiry/chronology negatives; original chronology regression failed before correction | Synthetic stored verifier facts over a real root, not actual native password/TOTP/provider validation |
| Issuance action comparison at `3995303` | Additive normal/race/restored 2 entries each; action-equality and consumed-bit negatives; vet and pinned lint | Request-action comparison only; later password snapshot/method-vocabulary changes require their own review |

The login freshness prerequisite already landed separately at
`09d441ba4cadae9b2658f4673b1a4e0e7d78dfce`; its existing independent and fresh landed
evidence is preserved in [the login record](password-signin-freshness-20261008.md).
It was not rerun or relabeled by this component work. Historical interrupted or
timed-out runs remain not passing.

## Current source and review work

Native store/session source at `166d033`, corrected through `01d23b2`, received
independent finite clearance: source selections 15/15/15 and the final-row guard
selection 9/9/9 normal/race/restored, plus independent bounds, denial, insertion,
commit-loss and durable-refresh checks with meaningful restored negatives.
Initial idle-refresh, terminal-completion, cancellation and final-context failures
remain recorded as failures before correction. This does not cover every later
store branch: the magic INSERT timestamp fix `6b0a540` and reserved-row admission
mapping `54511c3` require their own review.

Workspace source `0e009de`, with test corrections through `e434bab`, received
independent normal/race/restored store 8/8/8 and personal-bootstrap 5/5/5 clearance.
Earlier cleanup failures and two invocations selecting no tests remain non-evidence.
Current session provenance and complete operation policy remain caller obligations.

The copied primary snapshot and closed actor-method vocabulary at `8fada8f`
received additive independent 4/4/4 checks and a genuine wrong-action negative.
The separate TOTP issuance-method correction `f1f16c8` is outside that earlier
boundary and still requires its own exact review.

Email source `2ba08c7` with test correction `67621ce` was independently reported
as normal/race/restored 8/8/8, plus 3/3/3 atomicity/personal-workspace checks and an
intended personal-guard negative. The first test digest failed to reach the
intended confirmation branch and remains failed evidence. The review session
exited before its final consolidated receipt. A different reviewer subsequently
reconciled every retained run-log hash and source archive against the recorded
receipts and exact Git objects, without impersonating the prior reviewer or
rerunning the accepted component.

Native login/magic/MFA/primary source through `37e31f9`, with selected-test
propagation at `039f17b`, remains under independent review. The earlier `e57b001`
selection of ordinary registration, sign-in and magic-link flows passed seven
independent runtime events without failed or skipped tests. This is a selected
functional result, not whole-suite or whole-producer clearance. Original magic
SQL failures are retained, and the corrected queries were reached by that selection.

A platform review blocked an assurance-policy regression during the prior
independent session. That operation remains unverified and is not being retried
or replaced. Separate permitted functional checks explicitly exclude it. The full
native suite must not be described as passing, and this coverage gap remains a
batch gate.

Recovery source `e5cee25` adds native request, preview, reset completion and password
change with the separately reviewed completion-policy design at `ad0a00c`.
Author pure/race selections passed 38/38 events, with vet and pinned lint passing.
Required-service source covers guarded reset request/completion and password change
with a real hasher. Independent ordinary runs through `301e8a9` passed 6/6/6
normal/race/restored events, after correcting test chronology, preflight denial
mapping, encoded cookie binding and root-backed handler availability. Earlier
setup, 401 and 503 failures remain failed evidence. Typed preview follow-up
`5ba2351`, complete failure/wait schedules and actual local completion-policy
qualification still require their applicable review. Its author used no runtime fixture. The delivery
wrapper terminal-error correction `2406043` likewise requires additive review.

The existing session reader sample at `56b30ae` and native MFA Status use at
`8e9cff8` received finite source review. Status pure checks passed 27 events;
actual sampled-status runtime remains unverified. The ordinary native selection
at `88856c3` separately passed 14/14/14 normal/race/restored events; none of those
results includes the blocked assurance regression.

Private read composition at `22ef06c` follows the independently reviewed design
`cec5873`: one privately constructed runtime/root/session/resolver, existing
repository and renderer, final current authority and commit-only buffered output.
Author pure/race selections passed two events each; vet and pinned lint passed.
It has no exported host constructor or listener. Required-service source uses
synthetic account/session setup with actual middleware; independent source/runtime
review, the full sign-in-to-business journey and commit-loss schedules remain open.
The fixture extension received independent source qualification only; a fresh
isolated runtime and actual checks are separate gates.

## Additional finite checks recorded 2026-10-09

Subsequent independent receipts close several earlier component review items,
without accepting the complete batch. Native ordinary selection at `0f361ee`
passed normal/race/restored 14/14/14 events and pure selections 36/36/36.
Recovery typed/HTTP preview at `728e126` passed ordinary 7/7/7, including repeated
preview and GET/HEAD without token consumption. Private read composition at
`22ef06c` passed ordinary 6/6/6 with actual native middleware and scoped retrieval.
These do not cover the complete writer graph, public-principal/two-handle
composition cases, full failure/wait schedules or actual commit loss.

Label-only corrections `9c48a5b` and `525f846` received independent review
without rerunning unchanged production checks. All 44 reviewed component blobs
were independently compared with integrated candidate `004a499`. Earlier failed
runs remain failed evidence; source identity does not promote runtime coverage.

The private source HTTP contract `785915d` received separate early design review.
Its implementation `e408f31` received independent finite source review, 42
normal and 42 race pass events, vet and pinned lint with zero issues. Independent
commit-marker and publication-cancellation mutations failed their intended
assertions; exact restorations passed. The response collector bounds body storage
and validates header publication budgets, replaces native errors with fixed
escaped messages, preserves HEAD and full/fragment behavior, and releases source
bytes only after the private read returns successful completion.

The distinct ordinary native HTTP suite at `21e7c17` received independent exact
source review and actual PostgreSQL-backed native middleware checks: eight normal
and eight race pass events, with zero failures or skips. The seven cases cover
literal listing, escaped fragment detail, HEAD, foreign scope, missing session,
canceled request and stored digest corruption. The reviewer used fresh synthetic
account setup; this is not a complete native signup-to-business journey or a
failed-commit schedule. A placement failure before resource creation remains
failed evidence; the diagnosed placement-only retry passed. All five owned
fixture resources, its run directory and watchdog were verified removed.
A listener, complete host and provider remain unqualified. The existing runtime
configuration forwarding heuristic remains disclosed; the new HTTP
production/tests and contract passed their scoped public scan.

The additive repository adapter at `eccb9c2` received independent finite source
review after its `28da29e` design review. Fifteen new pure normal and fifteen race
pass events, vet and zero-issue pinned lint qualify construction, exact retained
interface forwarding and failure handling. A typed-nil guard removal failed for
pointer and map values; exact restoration passed four events. The required-service
test visibly failed without its prerequisite, with zero skips. A fresh independent
run on integrated `0f514e9` then passed seven normal and seven race events for the
legacy and DBTX paths: scoped discovery, committed revision/activity/draft writes
and caller rollback. The changed legacy adapter also justified a focused ordinary
HTTP regression run, which passed eight normal and eight race events. All five
fixture resources, its run directory and watchdog were verified removed. The
legacy SQL transaction path keeps its existing caller ownership.
A real transaction signature adapter cannot qualify operation-callback invalidation
or establish current authority. No executor is enabled by this repository change.

The baseline presentation contract `04f8620` and source `af398c6` received
independent finite design and source reviews. Thirty-seven normal and thirty-seven
race test events, vet and zero-issue pinned lint cover the typed views, ordinary
forms, raw input bounds, alias isolation, final positive revision, stale drafts,
finite guide input and bounded output. Independent raw-draft preflight and output
cap mutations failed, with exact restorations passing. Source-author escaping and
human-decision mutations also failed their intended assertions and were restored.

The `af9d422` layout and static-browser harness correction received separate
review after an actual narrow-screen overflow failure. The corrected harness
passed on seven synthetic file documents with JavaScript disabled: keyboard
access, 390/1024/1440 widths and CSS text scaling at 200% and 50%. CSS scaling is
not browser zoom. The harness substitutes only the hashed local stylesheet path;
it exercises no HTTP, authentication, form submission, persistence or HTMX.
The human decision stays disabled and drafts have no sending control. These
renderers do not enable a business host or prove that their forms are bound.

The private baseline reader at `6d7ef6d` received independent design, source
and required-service review. Its ordinary pure checks passed 101 normal and 101
race test events, including the focused source-HTTP regression checks after the
shared read-path change. The repository draft correction separately passed five
normal and five race events. Only absence of the initial saved-draft row is
optional; a saved draft with a missing case or historical source fails
unavailable. The original behavior and independent fix-removal mutation failed
the intended assertions, and exact restoration passed. The guide distinct-source
bound mutation also failed and was restored.

On a fresh independent PostgreSQL fixture, baseline reads passed 25 normal and
25 race events; ordinary source HTTP passed eight in each mode. The actual
missing-historical-source regression failed when the production fix was removed,
and all 25 baseline events passed after restoration. This exercises scoped
lists/details, the five guide topics, saved and stale drafts, missing/foreign
records, cancellation, corrupt sources and the 100/101 distinct-reference bound.
It does not establish a complete native producer graph or public host.

The private baseline HTTP adapter at `3f2a4b6` received independent exact source
review after its `2737a09` design. Its pure checks passed 126 normal and 126 race
events, including 42 existing source-HTTP events needed for the shared collector
change. Vet and pinned lint passed. Independent unknown-presentation and wrong
error-section mutations failed; exact restorations passed. The adapter admits
bounded GET/HEAD selectors, obtains form tokens from the same native session
service after admission, and uses the existing buffered publication path.

A distinct fresh PostgreSQL fixture passed 21 baseline-HTTP normal and 21 race
events. The extracted test helper and changed response collector justified
focused baseline-read 25/25 and source-HTTP 8/8 regression events on that exact
head. All runs had zero failures or skips; missing prerequisites separately
failed visibly. These are ordinary private-handler checks with synthetic native
session setup. They do not exercise a real browser, public listener, POST
mutation, failed commit or full signup-to-business journey. Those gates remain
open. Fixture cleanup and exact source identity are recorded independently.

The additive operation persistence contract and shared audit types received
independent finite design and source review. The parent audit checks passed 29
normal and 29 race events, including three existing checks justified by the
shared validator change. The operation store production source at `e5fe632`
passed 132 normal and 132 race events, vet and zero-issue pinned lint. Independent
request-binding and result-snapshot mutations failed their intended assertions;
exact restoration passed. Required-service test additions through `4d003f1`
received separate source review, including specific database error codes for
invalid UTF-8 and malformed JSON, and an observed database lock wait between the
two existing connections.

A fresh independently operated fixture passed 17 normal and 17 race store events
at `4d003f1`, and 19 normal and 19 race audit events at `bd8319a`, with zero failures
or skips. These cover exact replay bytes, escaped NUL and large JSON numbers,
conflicts, rollback, pending completion, reservation bounds and shrinkage,
concurrent same-key storage, shared audit insertion/readback, legacy nullable
operation identifiers, finite binding/privacy constraints and runtime privilege
denials. Audit tests use synthetic attribution and do not qualify successful
native-context attribution. No executor or provider dispatcher is enabled.

The fixture independently checked restricted column privileges and actual
permission denials. In a separate owned schema, the exact ordered SQL for
migrations 1 through 17 applied successfully; five owner-allocation rejection
cases, explicit increases and rollback checks passed. This is ordered SQL
evidence, not execution of the migration ledger, a historical-data upgrade, or
concurrent owner allocation. Source checks preserve every original migration ID
and effective SQL hash through 16. Generated-application verification failed at
the existing framework file limit; its owner boundary remains preserved and the
generator gate remains open.

The ordinary browser test remains unqualified. Its first run at `220b295` failed
before producing any coverage. The separately reviewed finite diagnostic at
`1c14f3` also failed, reporting the browser-launch phase and zero page/layout/form
counts. Race execution did not follow these failures. The first launch-only diagnostic
returned an inconclusive category. A separately reviewed follow-up identified a
Chromium Unix socket path-length failure under the nested test temporary directory.
The proposed short private temporary root still requires exact source review and
selected runtime evidence; the diagnosis is not a browser success claim.

## Preserved limits and next gates

Required services fail when absent; fixture success never qualifies a live
provider. Distinct source authors and runtime reviewers retain serial fixture
custody, original expiries and exact cleanup evidence. A fixture schema-ordering
failure remains a failed source qualification; corrected source must pass its
unchanged catalog and privilege checks on a fresh fixture.

The complete selected writer/producer graph, actual wait/expiry/failure schedules,
private same-database composition, current workspace/resource policy, repository
retrieval, buffered HTTP/SSR, RB1–RB9 forms/local edits/unsent drafts and real
identity-to-business journey remain required. The integrated exact-head review
must cover every included coding scope before guarded merge and fresh landed
checks. Full SaaS, billing, generator, providers, portability, release and
restart/backup/restore gates remain in scope.

Public scanning is scoped evidence: the full scan retains 44 previously
disclosed findings and forwarding heuristics. The native store/session package
scan also reported three existing test credential-assignment heuristics; this
record does not claim a fully green package or repository scan. Recovery source
adds three credential-assignment heuristic matches on ordinary field equality
expressions; these are disclosed separately and are not a green scan result.

The private reader fixture test forwards its runtime connection credential through
the standard configuration type; the scoped scanner flags that one assignment.
No fully green repository scan is claimed.

The operation store and audit required-service tests add two scanner findings on
ordinary typed credential forwarding from private test configuration. Independent
source review classified those two as heuristic matches without literal secrets;
the failed scan result remains recorded and no suppression or fully green scan
is claimed.
