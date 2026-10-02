# ADR 020: Bounded reconciliation scheduling and complete snapshots

Status: accepted for implementation; live-provider qualification remains separate.
Date: 2026-10-02.

## Decision

A customer snapshot carries an explicit complete-collection bit, false by
default. A provider adapter sets it only after all bounded pages have been
read and validated. Reconciliation rejects incomplete, future or older than
five-minute observations. Two matching bounded Stripe reads remain evidence
of collection stability, not upstream atomic snapshot isolation.

An owner-configured scheduler drains only billing webhook jobs and bounded
reconciliation claims, with persisted keyset scan checkpoints for each immutable
provider/account/mode endpoint. Poll, scan-page, per-pass and provider limits
are finite. Cancellation stops the service; readiness and executable wiring
remain explicit integrator responsibilities. The native personal generator
does not silently enable subscriptions.

The integrator reserves additive reconciliation migration sequence 15 and
federation sequence 16 after immutable local evaluation migrations 1 through
14. Core foundation SQL is preserved. Configuration and credentials are
owner-supplied; no live provider, cloud deployment or subscription release
qualification follows from local tests.

## Verification boundary

Real PostgreSQL tests must cover scheduling, fencing and incomplete/future/stale
negative cases. Official SDK fixture checks qualify the adapter contract only;
provider qualification requires genuine configured account evidence.

Scheduled service composition must bind both the job repository and reconciler
claim selection to the exact configured installation, application, environment,
provider, account and mode. Scope is applied before deadline/lease maintenance
and before claiming work; rejecting a foreign job after claiming it is unsafe.
Scheduler endpoints and reconciler scopes must match exactly. Legacy direct
unscoped repository operations remain explicit compatibility surfaces, not a
scheduled service default.
