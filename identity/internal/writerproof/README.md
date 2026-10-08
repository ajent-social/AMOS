# Native W1 evidence

This identity-internal package implements the closed evidence factories in the
[writer API](../../../docs/contracts/current-session-writer-api.md). Workspace
and business packages cannot import it. Its concrete values have private state;
no external producer registry, arbitrary payload or evidence context setter is
provided.

Factories bind copied native snapshots to the exact live attempt and acquired
rows. Password and challenge evidence, actor bounds, enrollment/TOTP/counter
state, and federation flow/provider receipts retain their original deadlines.
An accepted TOTP window is derived from its original step. A counter denial has
its own thirty-second bound and never creates issuance. An unmatched code uses
AcceptedStep=-1; enrollment has no accepted step and uses LastStep=-1. For an
aal1 actor the effective assurance bound is its original absolute session bound;
an elevated actor retains its original effective assurance deadline.

Only sign-in, magic confirmation, successful TOTP and callback-login variants
can obtain provisional issuance. Its credential getter is consumed once;
non-consuming Check supports the identity store before and after that getter
while the same attempt remains live. Duplicate issuance/getter/finalization
terminates the attempt with unavailable rollback, including when a native caller
ignores the returned error. Exact final-time validation produces a terminal
Permit. Native publication must match both that permit and the root's single
committed release; neither value alone publishes a cookie or protected payload.

These factories are a trusted native boundary, not independent verification of
copied claims. Native adapters must actually perform password/TOTP/provider
verification and compare every held final row to its intended transition before
Finalize. Password change requires both original actor and primary evidence.
No native session staging, producer migration, provider validity adapter,
composed host or full writer graph is qualified by this package alone.

Pure tests cover the closed issuance map, zero inputs and strict original bounds.
`TestEvidenceRuntimeRequiredService` requires the separately reviewed writer TLS
profile through `AMOS_WRITER_RUNTIME_TEST_CONFIG`; missing prerequisites fail.
It exercises real PostgreSQL root/evidence/terminal lifetimes using explicitly
synthetic stored password rows, without claiming a real password verification or
native authentication journey. Its first finite runtime subset covers password
issuance and replay/finalization denial. Actual challenge, MFA, federation,
cross-attempt/service, expiry-wait and complete native transition schedules remain
integration gates. Pure equality faults are not measurements of real expiry.
