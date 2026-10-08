# ADR 035: Source presentation before authenticated composition

Status: proposed; independent exact-head review and guarded adoption required.

## Decision

Reuse the existing continuity repository for scoped source list/search/detail.
Implement the [pure source view contract](../contracts/continuity-source-view.md)
as an application-owned renderer of caller-supplied values. It performs no
retrieval, authorization, HTTP handling or effects. This avoids duplicating the
already qualified storage mechanics while current resource authority is resolved.

Six additive T-RPL-SOURCE-VIEW lifecycle rows track only this component. Its
preflight depends on the repository's landed gate; existing T-RPL-WEB preflight
also depends on its landed gate. Existing search, full web, host contract and
host acceptance are preserved, including current session/workspace/CSRF/resource
checks and actual browser journeys. Original tasks and the terminal are retained.

## Alternatives and consequences

Implementing a second search repository duplicates the bounded SQL and digest
checks already delivered. Attaching a handler now would conflate session
admission or person equality with current resource authority. Waiting for the
whole host would unnecessarily block independently testable escaping, bounded
presentation and ordinary GET form behavior. The pure component provides those
mechanics while every authenticated disclosure and browser gate remains open.

No visual redesign, shared API change, migration, provider call, installation
mutation, traffic cutover or product acceptance is authorized by this decision.
