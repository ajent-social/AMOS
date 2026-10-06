# ADR 025: Finite host protocol and business routing

Status: accepted for implementation after independent exact-head review and merge.
Date: 2026-10-06.

## Decision

Adopt [finite host routing](../contracts/host-routing.md) as semantic amendment
v1.14. Optional all-or-none protocol slots bind eight reserved MCP/OAuth paths;
an optional explicit method-neutral business manifest has exact paths, a home handler and prefix
routing with no catch-all. Handlers retain their own protocol and authorization
behavior. Existing method/path registration remains available before serving,
with its existing descendant-only prefix semantics.

Freeze the route set permanently at first handler exposure, valid Serve attempt
or direct runtime request. This intentionally changes late-registration behavior
to a stable error. Configuration is copied, reserved routes never fall through,
and aliases cannot select the new handlers. Bounded telemetry uses the same
routing decision and may decorate the response only with its existing request ID.

## Separation and ownership

Routing is independently implementable and testable without production database
qualification. Runtime-only PostgreSQL, role grants, TLS trust, production origin,
cookie/proxy policy and deployment remain later INT-HOST-02 stages. The operation
executor retains all v1.13 authority gates; custom trusted handlers do not gain
authority merely by being mounted. No identity, migration or database wire shape
changes here.

The integrator owns the exported API and delegates the exact source paths in
integration assignments. Six first-class delivery stages cover preflight,
implementation, verification, independent review, guarded merge and landed
checks; complete-product reconciliation depends on their completion. Existing
accepted component records are unchanged. Local routing checks are not protocol,
provider, production or release acceptance.
