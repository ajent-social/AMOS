# Cross-lane contract and ownership baseline

Date: 2026 09 30 UTC. Version: `amos-contract-draft-1`. Status: proposed; T1.2 must freeze this contract before dependent implementation dispatch. No Go interface or API here is claimed implemented.

## Authority and consumers

Read VISION, RFC 0001 and the assigned task contract. The public planning record contains no restricted source locators or consumer details. Runtime APIs enforce authority consistently for web, REST, API-key and MCP callers. Independent agent governance stays outside the product runtime.

The integrator alone owns root module/dependency files, the canonical OpenAPI bundle, shared identity/policy types, migration ordering, executable entrypoints, generated-registry wiring and required CI check names. Domain workers submit owned fragments and migration proposals; they do not silently amend shared contracts. A contract change invalidates affected unstarted tasks until the integrator updates both the contract and their acceptance criteria.

## Proposed package seams

| Area | Public extension surface | Private implementation ownership |
|---|---|---|
| Runtime | `app/` composition and business handler registration | `internal/runtime/` wiring and lifecycle |
| Configuration | `config/` schema and validated immutable configuration | CLI setup/planner adapters |
| Identity | `identity/` principal/session/authentication contracts | Method-specific packages and durable adapters |
| Workspaces | `workspace/` membership and authorization | Lifecycle, invitations and store adapters |
| Billing | `billing/` provider and entitlement contracts | Stripe adapter, durable intents, usage/seat reconciliation |
| Generated API | Versioned `api/` operation fragments and generated types | `internal/codegen/` plus transport implementation |
| Agent interface | `mcp/` adapter using a maintained SDK | OAuth/store integration and exposure registry |
| Business routing | `app/` registered integrated routes or `proxy/` private service adapter | Header stripping, identity binding, origin allowlists |
| Presentation | `ui/` default views, theme and override contract | Page-specific rendering and browser tests |
| Tooling | `cmd/amos/` stable commands | `internal/cli/`, templates and generation journal |
| Infrastructure | `infra/` owner-controlled programs/recipes | Provider-specific components and policy attachment |
| Operations | `observability/`, `operations/` contracts | Diagnostic pipeline, backups and recovery jobs |
| Maintenance | `maintenance/` job, verifier and policy contracts | Runtime adapters, evidence, rollout and upstream intake |

These are a baseline, not permission for two lanes to create competing models. Some task drafts use narrower package names; T1.2 resolves any mismatch before workers start. Generated consumer code imports supported exported packages, never AMOS `internal/` implementation.

## Semantic contracts to freeze

1. Principal: installation/application/environment, person or machine identity, current workspace, grant scopes, authentication assurance and current-state authorization source. Untrusted request fields cannot construct a principal.
2. Identity separation: personal workspace is a billing/resource container, not a global credential; organizations share resources only through verified membership. No cross-app email linking.
3. Policy result: allowed, denied or unavailable; stable reason code, evaluated policy revision and freshness/expiry. Products own resource semantics.
4. Operations: stable operation ID, request/response schemas, errors, pagination, side effects, idempotency, required permissions, entitlements, assurance and MCP exposure. Freeze exact OpenAPI version and generator SDK profile through evidence.
5. Database: transaction ownership, migration sequencing/checksums, immutable IDs, clock semantics, concurrency constraints, outbox/inbox and retention. Migrations are proposals until the integrator assigns the sequence.
6. Provider effects: durable intent, explicit unknown outcomes, provider idempotency bounds and reconciliation ownership. Do not claim exactly-once remote execution.
7. Business extension: integrated domain handlers versus privately routed upstream services; exact routing/prefix/cookie/redirect/streaming behavior and origin-authenticated identity propagation. Same-domain appearance never makes an upstream request trusted by itself.
8. UI: default templates, override lookup, view-model/API compatibility, escaping and security challenge flow. Upgrade tooling can identify incompatible customizations without overwriting them.
9. Deployment: owner-reviewed desired state, artifact/source identity, credentials, schema compatibility, release receipts and recovery. No mandatory hosted AMOS control plane.
10. Maintenance: immutable evidence/plan/artifact binding, separate proposer/verifier/releaser, policy version, spend/time/retry bounds, egress boundaries, rollback eligibility and manual suspension.
11. Privacy: allowlisted upstream diagnostic fields, bounded cardinality and size, source-path scrubbing, synthetic reproductions, private vulnerability handling, retention and owner disablement.
12. Compatibility: core/UI/generator/configuration/API/database/agent-client version matrix. OpenAPI generation is not protocol conformance evidence.

## Reserved shared files

`go.mod`, `go.sum`, `api/amos.openapi.yaml`, `api/policy.schema.json`, `config/schema.json`, root `migrations/` ordering/registry, `cmd/amos/main.go`, runtime top-level composition and root `.github/workflows/` gate wiring are integrator-owned until delegated for an exclusive task. Isolated worktrees do not eliminate semantic conflicts. Shared changes land before dependent tasks.

## Test and release seams

Domain code gets scoped behavior and denied-path checks. API tests hit the real registered boundary and assert status plus response; UI tests exercise actual pages and an edge case. Durable concurrency tests use a real test database and fail visibly when a required test service is absent. Provider fixtures cannot satisfy provider acceptance gates.

Task verification commands are prescriptions to implement and run, not current executable capabilities. No contract is execution-certified until the intended inexpensive agent tier completes it under these checks and evidence is reviewed. Shared integration checks and live deployment acceptance belong to E16; a unit task cannot claim the whole release shipped.

## Deployment and write authority

Planning is not infrastructure spending, publication or migration authorization. External prerequisites have named human/owner gates. AWS, Cloudflare, email, payment and identity-provider credentials never enter model-visible artifacts. Repository and cloud permissions are distinct; customer organization ownership cannot grant deployment authority.
