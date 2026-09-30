# AMOS roadmap

Updated: 2026 09 30 UTC. Public status only.

## Shipped

No product implementation or release is claimed.

## In progress

Initial architecture and exhaustive staged plan have been drafted and structurally reviewed. Accepted task evidence is preserved in `docs/planning/execution-state.json`; task checkboxes remain open until review passes.

## In flight

- T1.5: real API/browser test entrypoints.
- T1.7: Apache 2.0 license, notices, and contributor/security instructions.
- T2.2: application composition and shared/business routing.
- T3.1: identity policy drafting and independent review.

## Integrated foundation

T1.1, T1.2, T1.3, T1.4, T1.6, and T15.1 have accepted task evidence. The current integrated Go suite and vet pass; migration checks use real PostgreSQL. This is foundation code, not a released SaaS app. Execution state is the per-task authority.

## Planned

- M0 / E1: contract, provenance and real-boundary harness foundation. Owner: integrator.
- M1 / E2-E7: local generated personal paid-app reference. Owners: runtime, identity, tenancy, billing, UI and CLI lanes.
- M2 / E8-E10/E16: managed cloud, provider test-mode and baseline recovery qualification. Owners: platform and integrator.
- M3 / E3-E6/E11-E12: full selected identity, teams, billing models, generated MCP and alternate business service. Owners: application and interface lanes.
- M4 / E7-E10/E14-E15: both-profile operating evidence, explicit profile migration, upgrade PRs and independent security gates. Owners: platform, upstream and reviewers.
- M5 / E13-E16: bounded private/upstream autonomous maintenance and full release acceptance. Owners: maintenance, security and integrator.

## Blocked

Live/provider/publication tasks retain their explicit owner-access, budget, provider onboarding, license and release-authority gates. The local foundation can be prepared independently. No blocked gate is reported as completed by planning.

See [plan](plan.md), [open decisions](planning/open-decisions.md) and [resume instructions](RESUME.md).
