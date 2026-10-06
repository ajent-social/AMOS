# ADR 023: Shared operation invocation and replay boundary

Status: accepted design, independently reviewed and landed as v1.12. ADR 024 defines the subsequent codec binding amendment. Runtime dispatch and source acceptance remain held.
Date: 2026-10-06.

## Problem

T2.8 requires one current authorization and durable invocation boundary for all
transports. The existing policy names are frozen, but no callable policy package,
operation registry or transaction adapter implements that boundary. The former
`app/policy/` task path conflicted with integrator-owned `policy/` and is removed
from worker ownership by this amendment.

## Proposed decision

Adopt the [operation invocation contract](../contracts/operation-invocation.md)
as the landed semantic amendment v1.12.
Keep the existing policy names and three decision states. Use one typed registry
and executor, trusted person plus current workspace context, transaction-time
resource/session/permission/entitlement rechecks and payload-bound durable replay.
Domain mutation, replay result, success audit and external intent commit together.
Provider calls remain outside the transaction. Current authorization is checked
again before replay disclosure; a previous allow never grants future access.

Required replay retains the original safe result with no implicit expiry.
Reserve bounded capacity before mutation and fail unavailable on exhaustion.
Secret or sensitive output cannot use this replay cache. A retention change needs
a separate decision. Typed handlers receive a narrow transaction-scoped context;
the interface is not a sandbox for untrusted application code.

The operation binding preserves the complete business descriptor (method,
canonical path, typed input/output names, features, policy, and MCP exposure) and
freezes an immutable operation revision plus input/output schema digests.
Required replay binds all of these version fields to the canonical request and
persists them with the result; a changed revision conflicts without rerunning
the mutation. The durable idempotency scope remains stable across revisions.
Capacity has an installation hard limit plus smaller aggregate actor and
workspace quotas; exhaustion is visible to the owner and remains unavailable
until the owner explicitly raises capacity. Results never expire or get
automatically deleted. Transactional effect adapters bind job scope to the
trusted invocation and reject incompatible effect metadata. Changed-key
conflicts are not audit policy denials and produce no invocation audit event.

## Implementation ownership and gates

The v1.12 semantic design amendment and its affected task/assignment records
landed together after independent exact-head review. T2.8
worker scope is limited to `app/operation/**`, beginning with registry and typed
metadata validation against the frozen policy contract types. The integrator must
separately assign policy implementation/adapters, invocation SQL storage, audit
vocabulary/schema, current-session recheck, migration content/registry, and
executable composition. Migration 17 is a candidate only until the integrator
allocates it; existing 1–16 bytes remain immutable. Executor dispatch and
provider/runtime acceptance stay held until compatible adapters and SQL qualify.

Review must reconcile typed descriptor fields, callback transaction capabilities,
current identity/selection APIs, finite audit data, output privacy, full replay
scope, explicit lock order/deadlock handling, and revocation serialization. The
current workspace store locks person before workspace and multiple resources in
ascending ID order; the invocation order extends that with session, membership
and role, billing, declared resources, capacity, idempotency, durable jobs, then
audit. Every resource touched by a callback must be in its pre-resolved lock set;
each composed adapter must comply or fail closed. Tests must use real database
transactions; transport integration and provider/release gates remain separate.

## Consequences and evidence

The larger transaction context explicitly amends a design-only handler proposal;
there is no implemented public operation handler API to migrate. Existing services
accepting a raw SQL transaction need explicit adapters. This proposal does not
qualify a registry, policy evaluator, generated transport, cloud deployment or
production release. T2.8 remains unaccepted until its full runtime criteria pass.
