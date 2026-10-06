# ADR 023: Shared operation invocation and replay boundary

Status: proposed; independent security and compatibility review pending.
Date: 2026-10-06.

## Problem

T2.8 requires one current authorization and durable invocation boundary for all
transports. The existing policy names are frozen, but no callable policy package,
operation registry or transaction adapter implements that boundary. The task's
`app/policy/` path also conflicts with integrator-owned `policy/`.

## Proposed decision

Adopt the [operation invocation contract](../contracts/operation-invocation.md)
as semantic amendment v1.12 only after independent review and integrator adoption.
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

## Adoption and ownership gates

This proposal changes no frozen contract yet. Adoption must update the frozen
contract version, the business-extension handler design, T2.8 paths and acceptance
criteria, and integration assignments together. The integrator exclusively owns
policy, SQL storage, audit vocabulary, migration sequencing, session recheck seams
and executable composition. Migration 17 is a candidate only; existing 1–16 bytes
remain immutable. Source workers must not allocate it themselves.

Review must reconcile typed descriptor fields, callback transaction capabilities,
current identity/selection APIs, finite audit data, output privacy, full replay
scope, deadlock ordering and revocation serialization. Tests must use real database
transactions; transport integration and provider/release gates remain separate.

## Consequences and evidence

The larger transaction context explicitly amends a design-only handler proposal;
there is no implemented public operation handler API to migrate. Existing services
accepting a raw SQL transaction need explicit adapters. This proposal does not
qualify a registry, policy evaluator, generated transport, cloud deployment or
production release. T2.8 remains unaccepted until its full runtime criteria pass.
