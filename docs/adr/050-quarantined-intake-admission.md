# ADR050: Quarantined diagnostic intake admission

Status: Proposed for independent review and integrator adoption as v1.37.
Date: 2026-10-17.

## Context

T14.13 qualifies a configuration declaration, not durable intake authority.
[Issue145](https://github.com/ajent-social/AMOS/issues/145) identifies missing
rolling quota, enrollment, deduplication, restore, wire, storage and HTTP decisions.
A local PostgreSQL fixture cannot invent these policies or effective cloud grants.

## Decision

Adopt the [intake admission contract](../contracts/upstream-intake.md) before
source implementation. Initial intake is anonymous-only, with enrolled requests
denied until a separately qualified issuer/verifier and current revocation exist.
Daily HMAC aliases resolve a stable private ledger identity across bounded key
overlap; every principal, alias and epoch record consumes the declared identity
capacity. Durable admitted attempts consume rolling budgets even on later failure.
Network overload/store failures before that boundary are not falsely called charged.

Keep payloads in immutable normalized SSE-KMS object quarantine, with PostgreSQL
metadata/reservations only and fenced publication. Separate actual object and SQL
acknowledgements from uncertainty; do not claim cross-service atomicity. Dedupe is
principal/profile scoped. A quota-denied duplicate is429 without receipt disclosure;
a quota-admitted duplicate can return its existing unexpired receipt without a
second object or retention extension. No invalid raw input or its digest persists.

Require an independently current, separately custodied witness with real metadata
verification, monotonic head/clock and a precise pending/finality protocol. This
service is UNQUALIFIED and its absence holds implementation/activation prerequisites;
a callback or verified flag cannot substitute. The initial profile has NO restored
snapshot activation. New epochs/operator approvals cannot recover lost budgets or
deletions. An older real restore must demonstrate denial, not simulated recovery.
Unknown prepared commits never clear on restored row absence. Required real
provider write-quiescence/fencing must prevent a late PUT from recreating a deleted
object before a reservation can be released; no AWS capability is assumed. Every
payload/receipt deadline starts at first reservation and is never refreshed.
Destructive privacy erasure remains possible during a pending hold without
releasing capacity or reactivating state. Only bounded opaque creation fences may
outlive diagnostic retention. Private owner-requested early deletion needs real
operator MFA/current authority and requester-target custody; collection is blocked
until that currently unqualified workflow is independently qualified.
Strict finite JSON/gzip/HTTP and expiry behavior are specified in the contract.

## Alternatives and consequences

Existing Go HTTP/gzip/JSON primitives and PostgreSQL transactions supply bounded
mechanisms but do not solve authority or object/database finality by themselves.
In-memory quotas, calendar-day resets, caller enrollment keys, global receipt
lookups and database-only restore epochs are rejected. Storing payloads in
PostgreSQL or declaring object encryption through a mock would silently replace
the accepted storage profile and is rejected. No generic workflow engine or new
runtime implementation is introduced by this design.

Fail-closed capacity, rotation and witness holds reduce availability deliberately.
No production witness, account, secret provider, bucket, KMS key, endpoint or operator
is selected. Initial payload backups/versioning/replication are disabled until
retention/restore enforcement is qualified. A future restore activation protocol
requires independent review. Source admission still needs exact SQL/witness wire
artifacts and real required-service evidence. T14.4 remains unaccepted; collection,
external sending, private handling, trusted jobs and publication remain disabled.
