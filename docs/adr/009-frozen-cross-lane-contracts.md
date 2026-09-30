# ADR 009: Freeze cross-lane semantic contracts

Date: 2026-09-30 UTC. Status: proposed for root review. Contract status: `amos-contract-v1` frozen for implementation, review pending.

## Context

Independent implementation lanes need stable authority, policy, persistence, route, configuration, and operation metadata semantics. The planning draft named these areas but left their exact shapes and rejection rules open. This ADR resolves the minimum shared v1 surface before dependent implementation dispatch.

## Decision

Adopt the exact exported Go surface and semantics in [`docs/planning/contracts.md`](../planning/contracts.md). The server constructs principals only after credential proof and current-state authorization lookups. Policy evaluation returns `Allowed`, `Denied`, or `Unavailable`; unknown state fails closed without being mislabeled as a denial. Each operation explicitly carries permissions, entitlements, assurance, side-effect, idempotency, and MCP exposure metadata. Strict Draft 2020-12 schemas and accepted/rejected fixtures enforce required fields and reject unknown fields.

Use OpenAPI 3.2.1 for the root API description and domain fragments, with the JSON Schema dialect defined by that OpenAPI version. The official OpenAPI Initiative identifies 3.2.1 as the latest published version as of this decision ([specification](https://spec.openapis.org/oas/v3.2.1.html), [version index](https://spec.openapis.org/oas/)). Generator and SDK versions remain an integration qualification item.

Persistence uses immutable UUIDv7 IDs and UTC `timestamptz`. Product lanes own migration contents; the integrator assigns global sequence and registry ownership. Route roots listed in the contract belong to AMOS and normalized collision checks fail startup. Configuration states its environment and deployment profile, represents credentials by references, and models provider unavailability explicitly. AWS profile choices are frozen to managed containers plus managed PostgreSQL or a single VM plus PostgreSQL. Cloudflare proxy behavior remains a qualification gate.

Contract amendments require an ADR, integrator review, contract version change, and updates to dependent unstarted tasks before dispatch. A wording-only clarification may use a patch revision if it changes no wire or storage meaning.

## Consequences

Consumers can implement against a single shape, while shared Go files, generated root OpenAPI, migration ordering, entrypoints, and CI remain integrator-owned. Schema validation provides design evidence only; it does not prove runtime enforcement, provider qualification, deployment, or release readiness. Original-work license and reuse provenance are unresolved. Root review is pending; this ADR does not assert certification.
