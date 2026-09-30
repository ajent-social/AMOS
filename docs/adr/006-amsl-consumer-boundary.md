# ADR 006: AMSL composition with evidence-driven upstream improvements

## Status

Proposed adoption policy

## Date

2026 09 30

## Context

Existing public capability contracts and implementations may reduce duplicated engineering, but candidate maturity does not establish AMOS composition guarantees.

## Decision

Qualify and pin AMSL mechanisms where useful; keep SaaS composition, UI, tenancy/commercial policy and maintenance orchestration in AMOS. Improve AMSL only for demonstrated reusable gaps, preferring standard/mature solutions first. Use reproducible public consumer evidence and upstream review.

## Consequences

AMSL and AMOS release authority remain distinct. No automatic promotion of library maturity, hidden runtime dependence or speculative framework extraction. Restricted source requires separate publication clearance.
