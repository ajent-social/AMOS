# ADR 007: Complete planning with gated parallel execution

## Status

Accepted delivery direction; scheduling mechanism proposed

## Date

2026 09 30

## Context

A broad architecture must survive session loss and support inexpensive implementation agents without pretending future tasks are already qualified.

## Decision

Inventory all requirements and granular task contracts now. Require dependency/ownership/acceptance validation, stage revalidation and explicit external gates. Support up to sixteen isolated execution workers with an integrator owning shared files. Separate implementation, integration, provider acceptance and deployment evidence.

## Consequences

Later task contracts remain provisional and uncertified. Current capacity and build leases can reduce concurrency; task count is not delivery-time evidence. Never bypass review to satisfy a calendar target.

## Boundary clarification (2026-10-03)

Each enrolled code-change lifecycle has one canonical scheduler and admission
authority. Review and any bounded fix/re-review are executable ordinary
apply-and-claim tasks. Coding ends at PR URL and exact-head handoff. Every
code-changing PR receives independent exact-head review; ordinary descendants
wait for reviewed merge and verified landing. Explicit speculative dependencies
may start earlier without release authority. Shared generic plan/apply/claim
representation and routing remain reusable, with lifecycle-specific admission
and result handling supplied by the owning service. Product delivery and
qualification remain separate. This clarification changes no task acceptance.
