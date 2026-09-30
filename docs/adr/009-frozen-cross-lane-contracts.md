# ADR 009: Freeze cross-lane semantic contracts

Date: 2026-09-30 UTC. Status: proposed for root review. Contract status: `amos-contract-v1` frozen for implementation, review pending.

## Context

Independent implementation lanes need stable authority, policy, persistence, route, configuration, and operation metadata semantics. The planning draft named these areas but left their exact shapes and rejection rules open. This ADR resolves the minimum shared v1 surface before dependent implementation dispatch.

## Decision

Adopt the exact exported Go surface and semantics in [`docs/planning/contracts.md`](../planning/contracts.md). The server constructs principals only after credential proof and current-state authorization lookups. Policy evaluation returns `Allowed`, `Denied`, or `Unavailable`; unknown state fails closed without being mislabeled as a denial. Each operation explicitly carries permissions, entitlements, assurance, side-effect, idempotency, and MCP exposure metadata. Strict Draft 2020-12 schemas and accepted/rejected fixtures enforce required fields and reject unknown fields.

Use OpenAPI 3.1.1 for the root API description and domain fragments as the compatibility baseline pending generator qualification in T2.1. The official specification defines the selected version and its JSON Schema-based Schema Object dialect ([OpenAPI 3.1.1](https://spec.openapis.org/oas/v3.1.1.html)). Although OpenAPI 3.2.1 is newer, that fact alone does not establish support in the selected Go generator. Generator and SDK versions remain an integration qualification item.

Persistence uses immutable UUIDv7 IDs and UTC `timestamptz`. Product lanes own migration contents; the integrator assigns global sequence and registry ownership. Route roots listed in the contract belong to AMOS and normalized collision checks fail startup. Configuration states its environment and AWS deployment profile, represents credentials by references, and models provider unavailability explicitly. Development binds only to loopback and permits AWS/Cloudflare adapters to be disabled without relaxing user authentication; production requires both providers and their credential references. AWS profile choices are frozen to managed containers plus managed PostgreSQL or a single VM plus PostgreSQL. Cloudflare proxy behavior remains a qualification gate.

Contract amendments require an ADR, integrator review, contract version change, and updates to dependent unstarted tasks before dispatch. A wording-only clarification may use a patch revision if it changes no wire or storage meaning.

## Consequences

Consumers can implement against a single shape, while shared Go files, generated root OpenAPI, migration ordering, entrypoints, and CI remain integrator-owned. Principal authority is exposed through immutable values with private fields and read-only getters; only trusted identity plumbing constructs and attaches a resolved principal. The owner selected Apache-2.0 for the project. Schema validation provides design evidence only; it does not prove runtime enforcement, provider qualification, deployment, or release readiness. Root review is pending; this ADR does not assert certification.
