# Owner-hosted runtime replacement qualification

Date: 2026-10-03. Status: owner-directed delivery gate; evidence pending.

Current baseline: 57 of 257 planned tasks are accepted. The generated personal reference app has local database/browser evidence. Complete organization, live payment/provider, cloud deployment and migration evidence remain open.

AMOS remains the owner-hosted application foundation. A separate customer-runtime service may be retained while AMOS proves an equivalent owner-controlled journey. This record does not claim provider readiness, deployment, migration compatibility, or permission to retire an existing service. The frozen AMOS contracts and task acceptance registry remain authoritative; this checklist adds composed evidence gates without marking tasks accepted.

## Outcome and boundaries

An application owner can create an installation, serve customers at the application domain, administer people and workspaces, collect and enforce payment, recover from faults, and maintain the application without a required shared customer-runtime dependency. Development control planes and pull-request agents may operate independently; their outage cannot interrupt customer requests. An external agent runtime remains independent and uses the application's standard interfaces.

## Proof sequence

| Gate | Observable evidence | Dependency |
|---|---|---|
| RQ-1 Local reference | Fresh generated app completes signup, explicit verification, sign-in, recovery, personal workspace, protected business action, sign-out and denied/revoked cases with real PostgreSQL and browser checks. | Current accepted components plus composed acceptance. |
| RQ-2 Identity and organizations | Selected login and MFA methods, account lifecycle, organization creation/invites/membership/roles, tenant isolation and current-state revocation work through the generated app and API. | Identity/workspace/UI tasks and actual provider prerequisites. |
| RQ-3 Paid journey | Owner configures a real test catalog; checkout, verified webhook, reconciliation, paid and denied business actions, cancellation, retries and recovery complete against the supported provider. Personal and organization billing, then seat/usage models, receive separate evidence. | Billing policy/composition and provider test mode. |
| RQ-4 Owner operations | One supported AWS/Cloudflare profile deploys the exact qualified artifact with app-owned domain, secrets, health, backup/restore and rollback evidence. The second profile is qualified separately before advertised. | Infrastructure, operations and release tasks. |
| RQ-5 Consumer transition | A willing existing consumer has an inventoried data/contract mapping, consent and export rights, dry-run import, reconciliation of identities and financial state, bounded dual-operation window if needed, rollback plan and current customer-journey checks. | RQ-1 through RQ-4 and a consumer-specific migration contract. |
| RQ-6 Retirement decision | Owner reviews retained-service consumers, data/retention duties, open provider events, support obligations, cost and recovery evidence; explicitly authorizes any traffic cutover or shutdown. | RQ-5 for every affected consumer; no automatic retirement. |

Each gate records source revision, artifact digest where relevant, commands/requests, producer, observed result, missing prerequisites and reviewer in the existing evidence system. Local tests cannot satisfy live provider or deployed gates. A service health check cannot substitute for a complete customer journey. Revisit the plan and versioned contracts if a consumer's required semantics differ; do not infer migration parity from similar endpoint names.

## Immediate planning work

- RQ-P1, kind: planning, stage: preflight, deps: accepted registry and current contracts. Inventory the equivalent customer journeys and mark each implemented, locally composed, provider-qualified, deployed or missing, with exact evidence links. Acceptance: every advertised capability has a corresponding evidence state and owner.
- RQ-P2, kind: planning, stage: preflight, deps: RQ-P1. Select the first reference consumer and document its authority, data and payment-state mapping without copying sensitive records into this public repository. Acceptance: a reviewable migration contract names unresolved and irreversible steps.
- RQ-P3, kind: planning, stage: verify, deps: RQ-P1 and RQ-P2. Reconcile missing work with existing AMOS task IDs, then amend only the affected task contracts through the normal integrator process. Acceptance: no duplicate implementation lane or unowned shared contract change.

These planning IDs are local to this qualification record. They do not alter the 257-task execution registry or authorize a migration, provider call, deployment or shutdown.
