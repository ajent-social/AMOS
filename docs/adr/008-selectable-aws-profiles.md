# ADR 008: Selectable AWS deployment profiles

## Status

Accepted product direction; concrete provider implementations subject to qualification.

## Date

2026 09 30

## Context

Installations differ in their resource budget, availability requirements and willingness to maintain hosts. A single implicit infrastructure default hides these tradeoffs.

## Decision

Plan and support two profiles selected per installation: managed containers with managed PostgreSQL, and a smaller VM hosting the application and PostgreSQL. Both use required Cloudflare integration and owner-controlled accounts. Qualify concrete AWS components and document expected costs without inventing price guarantees.

## Consequences

Each profile needs independent deployment, isolation, update, backup and recovery evidence. The VM profile has a single-host failure boundary unless explicitly extended; it is not advertised as highly available. Profile conversion is an explicit planned data migration with recovery, not an automatic in-place switch. Delivery may qualify one profile before the other, but both remain in the complete plan.
