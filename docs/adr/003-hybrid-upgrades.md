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

## Boundary clarification (2026-10-03)

Release and upgrade profiles are immutable, versioned inputs to review. AMOS
generates reviewable upgrade proposals and preserves authored files. A
code-change lifecycle service owns PR safety and executable independent
review/fix/re-review/landing tasks; a product workflow controller owns delivery
and qualification. AMOS does not implement a second task/PR orchestrator and
remains useful during external workflow outages. This does not qualify upgrade
execution or production release.
