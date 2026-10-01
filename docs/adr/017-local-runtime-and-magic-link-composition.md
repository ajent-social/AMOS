# ADR 017: local runtime and atomic magic-link composition

Status: accepted integration boundary; deployment and complete auth qualification remain pending.

The generated local development profile runs one application environment in one
private PostgreSQL database. An additive immutable deployment binding records
installation, application and environment; startup must reject conflicting
pre-existing scope without moving or deleting records. Development cookies and
local mail capture require a validated loopback origin and actual loopback
listener. Local capture remains unqualified email delivery and stores only in
an explicitly private, ignored owner directory.

Magic-link challenge consumption and session rotation must commit together.
The session service exposes a caller-transaction operation; returned cookie and
CSRF values must never reach the browser before a successful commit. Proofs are
constructed inside identity code from fresh authoritative account, verified
contact and required policy checks. Magic-link email proof supplies AAL1 only.
Existing MFA/organization requirements cannot be bypassed or relabelled.

A separate optional digest on magic-link challenges binds the requesting
browser; plaintext cookies are not stored. Scanner GET/HEAD only previews.
Different-device confirmation requires an explicit user POST and the same
origin, challenge, policy and atomic session checks. Owned pending flow cookies
are capped independently of whether an address exists. Missing prerequisites
fail visibly; a configured read-only backend is rejected before address lookup.

Protected sign-in material and renderer output use the exact `/magic-link`
purpose path, separate from verification and password reset. The additive
migration also admits the distinct `magic_link` operation to durable rate limits.
Existing migration bytes and global ordering are immutable. The reference
application reserves sequences 8 (todos), 9 (verified webhook ingress), 10
(deployment binding), and 11 (magic browser binding); other generated application
assemblies must allocate their own consecutive entries through the integrator.

The locally authored flow does not claim imported or qualified AMSL reuse.
Timing privacy across mid-request backend outages and live mail-provider
qualification require separate obtained evidence.
