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

The distinct ordinary native HTTP suite at `21e7c17` shares the existing synthetic
setup and has compile-only and pinned-lint evidence. Its actual required-service
runs are pending. Test-only collector responses do not prove database completion,
native signup-to-business behavior, a listener or full host admission. The existing
runtime configuration forwarding heuristic remains disclosed; the new HTTP
production/tests and contract passed their scoped public scan.

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
