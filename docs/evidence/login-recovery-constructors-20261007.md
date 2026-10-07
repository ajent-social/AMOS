# Login and recovery transaction constructor components

These additive v1.16 leaves preserve legacy configuration, constructor and
operation behavior while admitting the bounded transaction runner. They do not
qualify production authority, a deployed host, mail delivery or a live provider.
The accepted-task registry and original acceptance scopes remain unchanged.

## Login: landed local component

PR42 reviewed `6f731a2d8b97b2c04380f3c1d87d45786bfee20f`, base
`4e7afabc28635be4e88e7f3e1cf0de6f4cf2e1f7`, landed
`3e701dfcb31cbf881c2a4ac2ff9f858ea8b87312`. All three source/test files match.
An independent non-author repeated 25 selected normal and 25 race entries, vet
and pinned lint. Nil/default/options mutations failed intended assertions and
exact-restored tests passed. The actual same-database runtime profile includes
identity, jobs, protected mail material and workspace dependencies. Seven normal
and seven race entries passed without skips: successful registration/confirmation/
signin/rotation and rollback, cancellation and real commit-failure cases.

A completion-only mutation ignored the direct transaction-completion sentinel
with a live context after real callback writes and real rollback-before-commit.
The success-suppression assertion failed as intended; cancellation and callback
rollback cases still passed. The assertion is attributed to the parent testing
object, so ordered subtest events identify the commit scenario; the subsequent
Go child-exit diagnostic is not counted as evidence. Exact-restored seven entries
and fresh landed seven entries passed. Missing fixture configuration fails visibly.

## Recovery: landed local component

Original source author `2383acab4e3d77021850b33622b060ac11dfa960` is preserved.
Candidate `d24364a1787c59bd24b6f9879aab95c9a2ba291c` in PR43 includes a
coordinator-authored single-line legacy test adapter from `service.cfg.DB` to
`service.db`. A different reviewer cleared the complete four-file candidate; the coordinator did not self-approve. Earlier
checks using a private adapter were conditional; final unadapted normal32/race32,
vet and pinned lint pass. Actual normal13/race13 pass without skips, covering
request, completion and password change across success, rollback, cancellation
and real commit failure. Synthetic proof/policy values in these component tests
do not establish production or current-session authority.

The independent reviewer repeated unadapted normal32/race32, vet/lint/caller build
and actual normal13/race13/restored13 with zero skips. Nil/default/options
mutations reached intended assertion failures, and the old legacy field produced
the expected compile failure; exact-restored checks passed. A completion-only
negative verified successful callback writes, actual Commit ErrTransaction and
live context before the intended success-suppression assertion failed. An initial
private observation wrongly included read-only preflight and is nonqualifying;
the corrected observation and restored checks are retained separately. GitHub internal errors temporarily held the ready/merge transition. After recovery,
ordinary guarded PR43 rebase merge landed `d5130d39e0e65358b2ccc7997889da3a801039e2`
on live base `83023dc38b8a31714a2f5a656c1ceb9808d7bec0`. All four files match
reviewed d24364a; a fresh landed runtime-profile run passed 13 entries without
skips. No protected check or branch policy was bypassed.

## Fixture and delivery boundaries

The private bounded fixture source had different-author exact-byte review,
53 offline checks and eight intended negative/restored checks before use. Real
TLS, explicit table grants, positive DML and 123 denial probes passed across
its finite profiles. Local fixture evidence does not qualify production roles.
Required service checks fail rather than skip when their profiles are absent.

Changed-source scanner flags were manually classified: empty-password validation,
configuration forwarding and synthetic fixture password construction contain no
private literal. This is not a whole-repository scanner-green claim. Hosted CI
remains unavailable under the existing billing constraint; no green status was
fabricated and normal merge protections remain enforced.

## Next dependency-ready work

The independently reviewed private handler completion design in PR41 landed
`83023dc38b8a31714a2f5a656c1ceb9808d7bec0`, preserving all four design files.
Its assigned three-file source leaf must invalidate/drain callback handles before
codecs and obtain independent source review, actual service and landed evidence.
Existing billing transaction runners need no replacement constructor. Paid-access
work still depends on complete T2.8 authority; production host composition still
needs explicit HTTPS/cookie/proxy, finite route, mail/key and schema-readiness
contracts plus real composed checks. Role, provider, budget and DNS gates remain.
