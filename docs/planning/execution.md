# Execution guide for up to sixteen implementation workers

Status: proposed dispatch procedure. No coding task is certified merely because it has a contract. Read the [master plan](../plan.md), [contract baseline](contracts.md) and assigned task.

## Authority and readiness

One integrator owns shared contracts, dependency pins, migration numbering, canonical OpenAPI, root executable wiring, CI gate names and plan status. A worker may implement only the assigned owned paths. Separate worktrees protect working copies but do not make overlapping schema changes independent.

Task readiness requires all dependencies accepted, a current frozen contract, resolved file ownership and no unsatisfied external gate. Later stages have complete draft task inventories because full-scope preservation is required; revalidate them at their stage boundary. A lower-reasoning worker does not decide unrecorded security or commercial policy.

## Code-change lifecycle handoff

Every code-changing PR requires an independent review at the exact proposed head. Review is an executable plan task claimed through the ordinary apply-and-claim loop, not a dashboard status or hidden controller step. Coding completes when the author hands off the PR URL and exact head. A blocking finding creates explicit bounded fix and re-review tasks. A negative review does not release the delivery gate; fixes must not depend on a successful original review task. Ordinary descendants wait for reviewed merge and verified landing. Only an explicitly speculative dependency may start earlier, and it has no release authority.

Each enrolled lifecycle has one canonical scheduler and eligibility owner. Shared generic plan/apply/claim representations and stage routing remain reusable, while the owning code-change lifecycle service supplies authoritative admission and result handling. A product workflow controller owns product delivery and qualification. The application foundation keeps its domain jobs and owner-local diagnosis/proposal/upgrade work and does not duplicate task/PR orchestration. A service outage does not make the application foundation unusable.

Use GPT-6-Luna, Claude Sonnet or another qualified inexpensive coding agent for bounded implementation. Use Astra/frontier review for novel identity, billing, proxy, release-policy and maintenance trust boundaries. Provider/model selection uses existing owner credentials and budgets, not a new billing mode chosen by a worker.

## Sixteen ownership lanes

| Lane | Responsibility |
|---|---|
| L01 | Integrator, shared storage/runtime contracts and final release acceptance |
| L02 | Generator, reference app, provenance and tooling |
| L03 | Identity persistence, sessions, password and lifecycle |
| L04 | Social/enterprise identity, passkeys, MFA and assurance |
| L05 | Workspaces, membership, invitations and authorization |
| L06 | Billing contracts, checkout, webhooks and subscription state |
| L07 | Seats, financial usage and reconciliation |
| L08 | Shared/default UI and replaceable client compatibility |
| L09 | Initialization, configuration, local development and CLI lifecycle |
| L10 | AWS infrastructure profiles and state |
| L11 | Cloudflare, deployment orchestration and network qualification |
| L12 | CI, artifact provenance, release packaging and upgrade PRs |
| L13 | Observability, backup, restore and operator workflows |
| L14 | MCP, OAuth, API-key transport and extension conformance |
| L15 | Private maintenance agents, verifier and promotion pipeline |
| L16 | Upstream diagnostics/maintenance and independent security qualification |

These are ownership queues, not permanently running processes. Only one task per lane is active. The wave table lists dependency-ready groups with no duplicated lane and at most sixteen slots. Human approvals/external account setup are not coding-worker slots. Under fewer available slots, select a compatible subset; do not change dependency order or launch unavailable capacity.

## Dispatch and integration

1. Check actual repository instructions, status, active claims and the current contract version.
2. Claim the task with the canonical atomic claim tool. Record its returned unique SHA; release only that owned SHA using the tool's compare-and-swap release. Ambiguous ownership is not permission to proceed.
3. Create an isolated task worktree using the accepted base. Read dependency outputs and exact acceptance criteria before editing.
4. Implement only the assigned capability. Test doubles remain in explicit test-only paths; production cannot fabricate identity, payment, deployment or provider success.
5. Run scoped formatting/lint, genuine allowed/denied checks and the task's verification commands. Demonstrate that the check detects its specified failure in an isolated mutation fixture, then restore it.
6. Submit a focused review artifact including what actually ran, limitations and proposed shared-file changes. Never publish private source context or credentials.
7. Integrator checks the exact proposed revision, resolves shared seams, reruns relevant composed checks and records acceptance. Required CI must match the final merge revision.
8. Release the task claim and update canonical status. A rejected contract feeds an explicit amendment and revalidation, not a silent model upgrade.

Do not auto-merge while a reviewer has an active blocking finding. Do not mix unrelated task edits into a shared commit. Follow the project's applicable directory/commit rules and release process at execution time; this planning pass did not create commits or authorize publication.

## Build and runner limits

Sixteen coding workers never means sixteen simultaneous heavy builds. On a shared host, check load before multi-package Go/lint/build work, hold when the one-minute load exceeds 10, acquire the configured shared build lease, and release immediately after the build using the exact returned lease identity. At most two heavy project build lanes are eligible; the shared lease may serialize them further. Never run more than one whole-tree race suite concurrently for this project.

Use ephemeral isolated runners for untrusted changes. Fork/third-party code must not execute with production credentials or on persistent privileged runners. Separate untrusted validation, trusted rebuild/signing and deployment identities. Pin reviewed workflow/action revisions. Provider and cloud acceptance jobs require their own scoped authorization and budgets.

## Task completion versus release completion

Task acceptance: code and intended checks work at the task boundary, negative evidence is reviewed, and integration into the accepted branch is verified. API changes require real requests asserting status/body; UI changes require browser golden and edge cases. Test-count/skip checks prevent a command that ran nothing from passing as evidence.

Release acceptance: E16 records all applicable provider, deployed API/browser, routing/isolation, recovery and compatibility evidence against the released artifact. If no production environment exists or authorization is absent, report that gate as not run/blocked; do not claim a shipped production feature. An earlier staged release may advertise only its independently qualified subset.

## Certification and escalation

All initial contracts are `Certification: NOT_RUN`. The intended cheap tier must actually execute each selected contract and produce reviewed green evidence before that contract gets a certification record. Mechanical validation establishes completeness and dependency consistency, not implementation correctness or model reliability.

Stop and report an explicit blocker when an acceptance criterion is impossible, a required interface differs, publication rights are unclear or a provider prerequisite is missing. Work on another ready task only after releasing/recording ownership correctly. Do not bypass policy or consume an unapproved paid provider to make a test green.

## Stage revalidation

Before S1, freeze core contracts and prove the smallest reference flow. Before S2, resolve infrastructure/provider accounts, profile choice and budget. Before S3, confirm all identity/financial/agent semantics against S1/S2 evidence. Before S4, prove state/override compatibility and recovery. Before S5, independently verify protected controls, artifact identity, privacy, cost limits and the ability to suspend automation. Re-render the task contracts when inputs change; do not dispatch stale generated instructions.
