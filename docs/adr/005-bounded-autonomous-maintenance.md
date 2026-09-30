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
