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
