# Quarantined intake admission contract

Requested batch ceiling: 25. Actual member: one bounded T14.4 design-admission
outcome. Original product implementation and acceptance remain open. Existing
engineering session selection is inherited unchanged; model name, token/cost and
stage-duration telemetry are unavailable. One author and a separate independent
reviewer handle this contract; no implementation worker fleet is implied.

| Batch | Member and outcome | Prerequisites | Ownership | Checks and delivery |
|---|---|---|---|---|
| intake-admission | T14.4: freeze the unresolved intake policy from issue145 | T14.1, T1.3 and T14.13 accepted; current frozen source reconciled | Integrator delegates only the named ADR and contract documents to one author | Public-safe links/consistency, exact-head independent review, guarded rebase merge and landed document/freshness checks |

## Scope and admission

[Issue145](https://github.com/ajent-social/AMOS/issues/145) records the unresolved
rolling budgets, scoped duplicate receipts, enrollment, restore activation,
canonical payload, durable quarantine and strict HTTP semantics. The existing
configuration design does not implement those boundaries. The candidate adds
ADR050 and contract v1.37; it does not create a collector or claim full T14.4.

The root task reservation is `d792b432d0c9946131caa7bc8f5281903c540567`;
integration reservation is `03af36270461216841680d26080698f0b67b5c05`.
Accepted baseline is `7de91f2c343727452b9759269e7986112de27d1b`.
Existing authored plan IDs and dependencies remain unchanged. The original
T-SDLC-14-4.1 preflight row owns design admission; implementation and its later
verification/review/merge/landed rows are not completed by this design.

Other dependency-ready rows were considered without editing their scope.
Full redaction and parser-fuzz qualification lack required runtime boundaries;
MFA recovery and seat reconciliation need further domain contracts. Ownership
baseline integration must resolve private material and module-digest semantics.
No acceptance was waived to fill the requested batch ceiling.

## Evidence

Candidate public-safe artifacts, changed relative links, diff formatting and
1607-task native projection freshness pass. Independent exact-head review, guarded
merge and landed verification remain pending. No source, service, PostgreSQL, IAM/KMS, enrollment, restore or
collection behavior is asserted. External collection remains disabled.

## Review map and acceptance boundary

The amendment must resolve every issue145 design topic: rolling source budgets
across rotation/restart; scoped duplicate receipts and denied retries; enrolled
authority remaining unavailable; independently current restore activation; finite
canonical projection and rejection privacy; restricted durable metadata/encrypted
object lifecycle; and bounded HTTP/commit/clock/retention semantics. Each must name
the source evidence required later. A proposed verifier, credential declaration
or callback is not actual authority qualification.

Documentation checks cover changed relative links, public-safe artifacts, diff
formatting and current native projection freshness. They do not execute the
future intake protocol. Future required-service tests and actual older-snapshot
restore tests are prescriptions, not passing results in this batch.

## Review obligations

- INTAKE-001: a late object write can recreate data after an absence/delete check.
  Require proven write quiescence or keep the reservation charged and intake held;
  provider deadlines cannot be inferred from cancellation of the client request.
- INTAKE-002: pending time cannot extend payload retention. Bind expiry at first
  reservation, with no refresh on READY publication or duplicate receipt.
- INTAKE-003: an older snapshot can resemble a rolled-back pending transaction.
  Row absence or a missing fence cannot authorize clearing independently pending
  witness state after an unknown commit.

These are concrete design review obligations, not observed runtime defects or
passing executable tests. Independent draft re-review confirms INTAKE-001 through INTAKE-004 resolved at
the design boundary. Final exact-head aggregate review remains pending.

- INTAKE-004: automatic expiry alone cannot honor an authenticated owner request
  for earlier deletion. Define the private authority/path/deadline or leave
  collection blocked pending separate qualification.

## Rejected design scenarios

These are reviewed document fixtures, not executed runtime tests.

| Proposed scenario | Required rejection |
|---|---|
| Timed-out PUT, absent HEAD, release capacity | Reject: a late write can still recreate the object; no provider quiescence proof |
| Publish READY after a long reservation wait with a fresh seven-day TTL | Reject: the first reservation fixed expiry |
| Restore old SQL while witness PREPARE is pending, then clear missing fence | Reject: absence is not independently current rollback authority |
| Treat receipt possession as owner deletion authority | Reject: receipt grants no read or operator capability |

The final independent reviewer must check each expected rejection against the
contract text and keep every unavailable runtime authority explicitly unqualified.

## Candidate checks

Author contract revision `974dd88923218afcd235540ddfaa5bf8e8ce4ebc` defines
the corrected protocol. Independent draft review by `generation_review` cleared
all four findings; no source/service/provider behavior was tested or inferred.
Public artifact and changed relative-link checks, `git diff --check`, and
`python3 scripts/check-plan.py --native-root .` pass. Authored plan source is
unchanged. No Go build, database or cloud job is needed for this documentation
change; runtime qualification remains explicitly NOT_RUN.
