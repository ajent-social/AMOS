# ADR 016: password mutation admission and protected reset material

Status: accepted for implementation. Contract amendment v1.6.

Durable admission is a resource budget and never grants credential authority.
Recovery completion binds a canonical `challenge_id` to one `reset:<id>` hash
operation. Cookie-authenticated password change binds current principal scope,
exact allowed origin and session CSRF before admitting one
`change-current:<person>` verification followed by one `change-new:<person>`
hash. The recovery handler must independently verify the current password before
replacement hashing, enforce current account and operation policy, and atomically
consume challenges/update credentials/revoke sessions. Budget consumption alone
is not successful verification. Recovery never issues a session, removes another
factor or overrides organization MFA policy. Missing policy support fails closed.
Password alone cannot be labelled AAL2; qualified step-up remains a separate gate.

Protected reset material shares the scoped encrypted store with verification but
uses a distinct write method and exact `/reset-password` action. Purpose-aware
resolution and final renderer checks prohibit crossing verification/reset paths,
including with a legacy resolver implementation. Tokens are not placed in outbox
payloads or ordinary logs. Links are bounded, expiring, purpose-bound challenge
references; GET/HEAD previews never consume them. The session-cookie contract's
no-URL rule continues applying to session tokens; challenge links do not carry or
issue session credentials. Challenge URLs must not be logged, reflected into
errors or sent through referrers/third-party assets.

Independent review cleared the finite operation/key admission and purpose checks
following correction of the legacy resolver gap. Real PostgreSQL and scoped
renderer/admission checks passed. These seams support the separately reviewed
recovery implementation; they do not qualify MFA, live email or deployments.
