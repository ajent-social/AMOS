# ADR 003: Upgradable core with replaceable UI and managed-file diffs

## Status

Accepted product direction

## Date

2026 09 30

## Context

Developers must customize presentation and business behavior while receiving shared improvements.

## Decision

Pin a versioned core/default UI and generated configuration. Support themes, per-page overrides and complete UI replacement. Preserve handwritten files; propose managed infrastructure/workflow changes using ownership and preimage manifests. Ship automated upgrade PRs.

## Consequences

Customized UI needs compatibility checks and developer maintenance. Unsafe file conflicts stop rather than overwrite. Database recovery and binary rollback are separate.
