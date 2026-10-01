# ADR 010: Atomic pending registration

Status: accepted for implementation, 2026-10-01. Contract clarification v1.2.

Signup must create a pending person and its personal workspace in one transaction.
The authenticated bootstrap API correctly requires an active principal, so it
cannot serve this earlier lifecycle boundary.

The identity store now returns an immutable `PendingRegistration` only after
successfully inserting the pending account. Its private transaction binding and
owner/scope identifiers are consumed by `BootstrapPending` in that same open
transaction. The workspace participant locks and checks the pending person and
uses the existing personal-owner uniqueness constraint. A zero value or proof
from a different transaction is rejected. No principal or session is created,
and all ordinary workspace authorization still requires a current active person.

Registration uses natural idempotency: the unique normalized email within the
installation/application prevents a retry from creating another person, and the
personal-owner constraint prevents duplicate workspaces. Public registration
responses remain generic; a duplicate must not alter the existing credentials,
rebind its owner, or issue a session. Verification/outbox failure rolls back the
entire registration. Provider delivery is a separate activation gate.

This adds an internal composition capability without weakening authenticated
bootstrap or changing storage/wire semantics. T3.5 consumes this seam and must
prove concurrent registration retry and unverified-session denial over real HTTP
and PostgreSQL before acceptance.
