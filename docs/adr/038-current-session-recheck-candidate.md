# ADR 038: Candidate current-session recheck boundary

Status: draft; not adopted. Source dispatch remains blocked on the finite gates
in the [v1.24 candidate](../contracts/current-session-recheck.md).

Keep request provenance private to the exact session service and return a
refreshed principal from a non-renewing caller-transaction recheck. This avoids
an application credential parser and avoids pretending a policy decision
refreshes the principal. Evaluate shared person/session locks against actual
writers before selecting an exclusive-parent protocol that can conflict with
issuance foreign-key protection.

The candidate fixes the API direction and exposes unresolved assurance,
producer/writer, same-database and resource-composition cases. Independent design
resolution and actual service qualification are required; this record neither
self-adopts an API nor enables an executor or continuity host.


The concrete [same-database composition](../contracts/current-session-composition.md)
and [writer/producer protocol](../contracts/current-session-writers.md) now supply
reviewable resolutions. The latter proposes database-wide serial writer
admission and a versioned producer root; this throughput and compatibility
tradeoff is not adopted by appending these links. Exact source ownership,
implementation and real-service qualification remain separate.
