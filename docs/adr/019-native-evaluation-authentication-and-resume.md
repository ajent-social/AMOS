# ADR 019: Native evaluation authentication and resumable private configuration

Status: accepted for the local evaluation composition; production qualification remains separate.
Date: 2026-10-01.

## Decision

The generated Go application composes shared authentication and business routes
on its configured loopback origin. Native forms call the same guarded controllers
as JSON clients; preview GET/HEAD requests do not consume email proofs. Successful
password reset redirects to a fresh sign-in GET. Strict-origin referrer policy
preserves same-origin form Origin checks while withholding proof-bearing paths.

TOTP mutations require the current browser principal, CSRF/Origin protection,
durable finite admission and primary-password verification. Concrete local factor
storage encrypts seeds with authenticated scope, purpose and expiry metadata.
Its key derives with a distinct domain from the private evaluation material key.
This local envelope is not a production secret-store or managed-key qualification.
The personal-only evaluation policy denies organization and enterprise contexts
until their full policy composition is implemented.

Session issuance samples the database clock once at the statement after primary
verification. That sample governs issuance, last observation and lifetime bounds;
a caller's earlier transaction start is not the verification time. The response
reports the assurance expiry actually persisted by the issuer.

The reference migration registry appends session assurance at 12, TOTP factors at
13 and finite MFA admission at 14 after the existing sequences. Existing published
foundation entries retain their IDs and SQL bytes.

Interrupted generation may reuse private randomness only from verified managed
journal preimages. It rejects changed, authored or untracked preimages. Private
runtime configuration and local process state are ignored application artifacts;
owner business/UI code remains authored and upgrade protection remains required.

## Evidence and limits

Scoped real PostgreSQL tests and the generated native Chromium lifecycle pass,
including explicit email confirmation, reset Back/resubmit rejection, TOTP replay
rejection and exact assurance expiry. Generated consumer compilation and private
path tests pass. Independent source reviews cleared the private path boundary,
MFA composition, session statement clock and managed resume boundary.

This records an evaluation composition decision. Live delivery, live Stripe,
organization authentication policy, production key management, cloud deployment,
operational recovery and complete release acceptance remain separate gates.
