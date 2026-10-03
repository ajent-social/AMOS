# ADR 005: Separate owner-private and shared upstream maintenance

## Status

Accepted direction; detailed policy defaults proposed

## Date

2026 09 30

## Context

Self-improvement must fix shared and application-specific defects without central access to private code or self-authorized deployment.

## Decision

Owner-controlled agents maintain private applications; a separate upstream system maintains AMOS. Default upstream diagnostics are sanitized allowlisted data. Separate proposer/verifier/releaser authority; eligible changes may automatically release and deploy under owner policy. Managed private maintenance is deferred.

## Consequences

Protected verification, spend limits, private vulnerability handling, recovery, kill switches and policy-change review are prerequisites. Agent-governance UI for application users remains outside AMOS.

## Boundary clarification (2026-10-03)

AMOS may diagnose owner-local issues, generate proposals and immutable-profile
upgrade changes, and run bounded domain jobs. It does not implement a second
task or PR orchestrator. A code-change lifecycle service owns PR safety and
canonical executable author/review/fix/re-review/landing tasks; ordinary
apply-and-claim executes those first-class tasks. A product workflow controller
owns product delivery and qualification. Every code-changing PR receives
independent exact-head review. Ordinary dependent work waits for reviewed merge
and verified landing; explicitly speculative inputs may proceed without release
authority. AMOS remains useful during external workflow outages. This
clarification assigns no implementation, provider or production readiness.
