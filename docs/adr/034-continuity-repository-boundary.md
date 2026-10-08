# ADR 034: Transaction-only continuity repository admission

Status: proposed; independent exact-head review and guarded adoption required.

## Decision proposed

Separate the first application persistence mechanics from the later authenticated
host contract. The [precise repository contract](../contracts/continuity-host.md)
accepts a caller-owned SQL transaction and four-part scope selector. It exposes
no credential parser, principal constructor, HTTP handler, executor or external
effect. A repository result is provisional until the authorized caller commits;
the caller must roll back on every repository error.

The application owns an unallocated SQL fragment and bounded typed methods for
stored continuity values. Tests may provision the exact fragment into a fresh
isolated database. Installation migration allocation, seeding and host wiring
remain separate integrator work. This adds no shared identity or operation API
and changes no frozen framework wire contract.

## Delivery dependencies

This amends ADR 033 only to allow independently reviewed storage mechanics before
full host composition. Six new T-RPL-REPOSITORY-CONTRACT lifecycle rows own that
review/adoption. T-RPL-DATA preflight depends on their landed gate and the landed
pure domain. The existing T-RPL-HOST-CONTRACT rows and their host consumers remain;
no host gate is accepted by the repository design. The original 1,501 records,
existing added task IDs and full acceptance terminal are preserved.

A later authenticated caller must prove current session, person, workspace,
resource, assurance and CSRF checks at the real transaction boundary. Passing
repository tests cannot establish those properties. Registered operations still
require the shared executor; no app-specific bypass is authorized.

## Consequences

Persistence can progress without pretending scope is authority or freezing an
unfinished host. Full replacement still requires every mapped workflow, composed
negative cases and isolated rehearsal. No existing installation, provider,
production data, traffic cutover or retirement is touched by this decision.
