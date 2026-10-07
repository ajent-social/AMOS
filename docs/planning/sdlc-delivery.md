# First-class SDLC and AWS production delivery

Updated: 2026-10-07. Delivery is in progress through `/ship`; completed stages require the linked journal receipts and do not imply production acceptance.

The owner requests the entire existing product scope to run in production at **https://amos.sire.run on AWS**, with bounded parallel agents. The current source-delivery batch uses **GPT-6-Astra low** only; provider calls, spending and deployment are excluded from this batch. The graph contains 1,233 delivery nodes: the original 1,200 stages for 200 baseline-unaccepted source tasks, 15 shared/production gates, three explicit backup review-fix stages, six finite host routing stages and nine runtime-only storage stages including three explicit review-fix stages. It supplements the 257-task product inventory without replacing product IDs or reopening accepted work. The original inventory, contracts and acceptance registry retain authority over product behavior and completed work. This is ordinary, unenrolled repository delivery; no lifecycle enrollment or external scheduler is established.

## Graph and execution contract

`docs/planning/sdlc-plan.json` is the local delivery dependency graph; the checkboxes linked below are its reviewable projection. It is not a portable consumer schema, Kazi goal file or running controller. Use one coordinator/scheduler for the original source tasks and their stage nodes, not two competing queues. Original `T*` tasks name deliverables; `T-SDLC-<epic>-<task>.2` performs their implementation, never dispatch both as competing coding assignments. Remaining product prerequisites resolve to `T-SDLC-<epic>-<task>.6`; accepted original IDs resolve only from current reviewed execution evidence. Reconcile the snapshot against live main and execution state at each lane boundary. If an accepted item is invalidated, append a scoped corrective chain rather than erase historical evidence. Any authoritative enrolled lifecycle found at preflight replaces the affected ordinary chain with canonical service tasks and combined delivery gates; never run duplicate merge schedulers.

Each unaccepted source task has six first-class stages: preflight, implement, verify, review, merge and verify-landed. All 200 source preflights depend on `T-PROD.15`, which follows requirements acceptance in `T-PROD.14`. These shared gates reconcile the existing approved scope, mapped use cases and documented privacy/regulatory constraints, then qualify crosscutting design contracts; they add no product scope and leave subsystem-specific designs with their existing task owners. Accepted tasks retain their existing historical review/acceptance evidence without invented retrospective stages. Existing source dependencies, owned paths, task contracts, provisional stage contracts and external gates are preserved. Preflight must resolve scope details before dispatch; explicit earlier project direction preserves the complete detailed inventory rather than replacing later work with outlines. Newly discovered scope gets rolling-wave decomposition: one dependency-triggered planning item per later outline until its horizon becomes reachable.

Verification covers actual changed behavior plus required CI, format/lint, schema/public-safety and proportionate security checks. Independent review records reviewer, candidate PR, covered task IDs, base/head SHA, findings and dispositions. A shared review is permitted only after an explicit plan reconciliation names every included coding task and preserves all verification dependencies. Merge uses GitHub REBASE on the approved head. Landed checks bind fetched main and obtained acceptance evidence. Blocked or negative review never counts as a passed gate.

For accepted findings append `T-SDLC-<epic>-<task>.2.F<n>` (stage: implement), `T-SDLC-<epic>-<task>.3.F<n>` (stage: verify), and `T-SDLC-<epic>-<task>.4.R<n>` (stage: review). Fix depends on the reviewed implementation and records the finding; verification depends on fix; re-review depends on verification and covers the new exact head. Pending merge depends on the latest successful re-review. Changed bases/heads invalidate affected checks. Preserve original findings and prior review attempts. Release findings follow the same rule with production-prefixed IDs and rebuild affected artifact/staging evidence.

## Parallel continuation policy

Use one coordinator and the owner-selected model for eligible workers, including independent reviewers. The current bounded batch uses Astra low with at most six project processes including the coordinator and two heavy checks. The original planning ceiling remains 16 ownership lanes. Recheck available capacity at execution; never claim inaccessible capacity or launch cloud coding fleets to manufacture slots. Fill freed slots from dependency-ready, claimed, disjoint work immediately; verification/review/handoff work also consumes slots. Schedule the critical path without starving other ready lanes. Workers have isolated task worktrees, caches and explicit leaf-path ownership on the authorized execution host. The integrator exclusively owns shared contracts, manifests, migration ordering, executable wiring and acceptance state.

Run a persistent eligibility loop during authorized `/ship` execution: reconcile -> claim -> dispatch -> collect -> verify -> independently review -> merge -> verify landed -> refresh successors. Continue across waves and context handoffs until `T-PROD.13` passes, not merely until coding or a release tag finishes. If a slot or build lease is unavailable, perform independent ready work. If every remaining item is blocked by an external prerequisite, persist exact blockers, owners, evidence and next runnable task; report that execution is incomplete. Never mark completion or clear policy holds to force progress. Resume from durable state when prerequisites return. Load team/crew and stage-specific skills just in time before actual multi-agent execution.

Keep acceptance evidence in the established execution registry for original product tasks. Keep new stage IDs in the companion stage journal; the existing product validator accepts original IDs only. Do not inject unknown stage IDs into that registry. Native plan parsing and manual `/ship` coordination are qualified for the authored graph; automated journal consumption must be qualified before relying on an unattended dispatcher. The [stage journal](sdlc-stage-state.json) holds obtained execution receipts; it is a companion journal, not a second product acceptance registry. Graph status and checkboxes mirror those receipts at reconciliation. An in-progress stage may record a separately admitted component slice without claiming the full task or all remaining dependencies complete. Stage receipts must contain task/status, owner/claim, dependency receipts, candidate/base/head/landed SHA or artifact digest, commands/results, independent reviewer/findings, blocking prerequisites and next action. A planning checkbox or graph dependency alone supplies no execution evidence. The coordinator persists this journal and reconciles it with source acceptance; no writable second product acceptance master.

SSD caches/artifacts follow repository instructions. Shared heavy-build load check and lease are mandatory, with at most two heavy lanes per project. Coding concurrency is not build concurrency. AWS/provider credentials belong only to qualified deployment/verification lanes; coding workers do not inherit production authority.

## Production scope and authority

Complete means every original required capability, both independently qualified AWS profiles, selected authentication/workspace/billing models, interfaces, upgrades and bounded maintenance satisfy release evidence. One owner-selected profile serves this installation at the target domain; qualifying the alternative does not require two competing production stacks. Maintenance remains disabled until its explicit policy and authority are configured and qualified. Customer migration, retirement or destructive cutover remains separately authorized; the public endpoint does not authorize unrelated customer actions.

The requested destination explicitly includes deployment in the eventual execution scope. `/plan` makes no provider calls, release, infrastructure changes or deployment. Do not ask for routine permission again during authorized execution; reconcile concrete account/region/profile, budget, DNS/provider access, production billing test limits and policy requirements early in `T-PROD.2`. Existing historical cloud-worker budgets are not silently reused as hosting authority. Missing prerequisites block dependent stages while independent implementation continues. Production actions are supported task stages with explicit outcomes (`implement` for release/deployment, `verify` for live/operating checks); unsupported `stage: deploy`/`release` tokens are not authored. Stage labels do not authorize a generic coding executor to publish or deploy: `/ship` must route these scopes to qualified release/AWS/provider capabilities.

## Capability bindings observed during planning

The local capability-profile resolver selected baseline, delivery and Go profiles and reported installed requirements satisfied. That is configuration discovery, not authenticated runtime qualification. No plugins were activated. `gh`, `aws`, `pulumi` and `kazi` CLIs are on PATH. Use `gh` for GitHub, `aws` for AWS, Go Pulumi programs for infrastructure and the configured Cloudflare MCP for DNS/provider operations. This turn exposes no callable Cloudflare tool; its deployment lane is blocked until a configured binding is qualified. No generic connector/browser fallback for Cloudflare mutation is approved. CLI installation proves neither account access nor budget/domain ownership. Kazi plan ingestion is unqualified; these rows carry opaque `acc:` criteria because the CLI is present, but `kind: agent` routes stage work through `/ship` stage skills, never blindly through Kazi. No Kazi goal or ingestion compatibility is claimed. Ordinary stage execution through `/ship` is the documented path; source contracts retain their authored acceptance unchanged.

`docs/design.md` is absent; stable architecture remains in VISION, RFC 0001, ADRs and frozen cross-lane contracts. A local code-graph database exists but freshness was not qualified; this refinement uses canonical inventory and direct document inspection. No product rediscovery, implementation or live deployment was performed.

## First-class delivery files

- [E1 delivery stages](sdlc/E1.md): 6 stage tasks for 1 unaccepted product tasks.
- [E2 delivery stages](sdlc/E2.md): 24 stage tasks for 4 unaccepted product tasks.
- [E3 delivery stages](sdlc/E3.md): 72 stage tasks for 12 unaccepted product tasks.
- [E4 delivery stages](sdlc/E4.md): 60 stage tasks for 10 unaccepted product tasks.
- [E5 delivery stages](sdlc/E5.md): 84 stage tasks for 14 unaccepted product tasks.
- [E6 delivery stages](sdlc/E6.md): 78 stage tasks for 13 unaccepted product tasks.
- [E7 delivery stages](sdlc/E7.md): 60 stage tasks for 10 unaccepted product tasks.
- [E8 delivery stages](sdlc/E8.md): 156 stage tasks for 26 unaccepted product tasks.
- [E9 delivery stages](sdlc/E9.md): 96 stage tasks for 16 unaccepted product tasks.
- [E10 delivery stages](sdlc/E10.md): 96 stage tasks for 16 unaccepted product tasks.
- [E11 delivery stages](sdlc/E11.md): 84 stage tasks for 14 unaccepted product tasks.
- [E12 delivery stages](sdlc/E12.md): 66 stage tasks for 11 unaccepted product tasks.
- [E13 delivery stages](sdlc/E13.md): 78 stage tasks for 13 unaccepted product tasks.
- [E14 delivery stages](sdlc/E14.md): 84 stage tasks for 14 unaccepted product tasks.
- [E15 delivery stages](sdlc/E15.md): 84 stage tasks for 14 unaccepted product tasks.
- [E16 delivery stages](sdlc/E16.md): 72 stage tasks for 12 unaccepted product tasks.
- [Production, requirements and operating gates](sdlc/production.md): requirements acceptance, crosscutting design-contract qualification, environment preflight, full coverage, immutable release, staging, independent release review, production rollout, live checks, operations and final acceptance (15 tasks).

## Definition of done

Only `T-PROD.13` is terminal. All of its prerequisites must pass with current source, artifact, real-provider and production evidence. The final report names the URL, release/digest, selected AWS profile, observed operating window and remaining limitations. Partial milestones remain partial.

## Dependency waves

Wave annotations are dependency-depth groups, not execution barriers or capacity promises. Runtime readiness plus current claims, ownership and build resources determine dispatch; workers refill continuously without waiting for unrelated members of a depth group. Markdown dependencies use parser-qualified IDs; the JSON graph preserves local IDs and records their projection IDs.

## Native delivery-stage index

The following split index is the explicit `/ship` planning target for delivery stages. Accepted original dependencies are historical seeds from the product acceptance registry, not new work. The ordinary product inventory remains a scope catalog; do not dispatch its rows in parallel with these implementation aliases.

### E-SDLC-SEEDS -- Preserved accepted product prerequisites

#### Wave 0

- [x] T1.1 Create the minimal module and test harness  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.10 Implement bounded durable jobs and transactional outbox seam  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.11 Implement configured email delivery adapter and truthful local fixture  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.12 Implement redacted tenant-scoped security audit sink  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.2 Freeze cross-lane semantic contracts  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.3 Build durable migration and transaction fixtures  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.4 Qualify dependency and publication provenance  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.5 Create API and browser verification harnesses  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.6 Enforce public artifact and secret hygiene  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.7 Record license and contribution/security policy  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.8 Wire formatter linter and shared validation entrypoints  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T1.9 Reconcile specialist contracts and certify first execution lane  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T10.1 Freeze operational signals and recovery targets  Owner: L13  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T12.1 Freeze business extension and reserved-route contract  Owner: L14  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T15.1 Freeze trust-boundary threat model and attack inventory  Owner: L16  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T2.1 Implement contract validation and generator command foundation  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T2.2 Implement application composition and route ownership seam  Owner: L01  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T2.3 Generate Go models and typed implementation interfaces  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T2.4 Create the reference business contract and durable resource  Owner: L02  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T2.6 Implement reference landing and business pages  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.1 Freeze identity/session/assurance contract  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.10 Implement external identity proof and linking coordinator  Owner: L04  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.15 Implement TOTP enrollment and challenge  Owner: L04  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.2 Create person and authentication-state persistence  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.3 Implement durable opaque sessions and auth context  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.4 Implement password hashing and credential verification  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.5 Add registration and password sign-in endpoints  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.6 Add email verification and delivery outbox  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.7 Bound authentication abuse and protect browser mutations  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.8 Implement password change and recovery  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T3.9 Implement magic-link sign-in  Owner: L03  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T4.1 Freeze tenancy and lifecycle policy  Owner: L05  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T4.2 Create workspace and membership persistence  Owner: L05  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T4.3 Implement atomic personal workspace bootstrap  Owner: L05  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T4.4 Implement current tenant selection at request boundary  Owner: L05  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.1 Freeze price, entitlement and financial lifecycle policy  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.2 Define provider capabilities and typed billing contracts  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.3 Create workspace billing ownership and durable intents  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.4 Implement versioned flat plan catalog and policy evaluator  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.5 Integrate Stripe customer and checkout adapter  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.6 Expose checkout request and status endpoints  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.7 Implement signature-verified durable webhook ingress  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T5.8 Implement authoritative subscription reconciliation  Owner: L06  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T6.1 Freeze shared route, view and accessibility contracts  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T6.14 Implement themes and individual template overrides  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T6.2 Implement default page shell and renderer seam  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T6.3 Build signup, verification, password login and reset pages  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T6.4 Build personal workspace shell and selection flow  Owner: L08  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.1 Specify initializer input and output contract  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.2 Implement initializer validation and safe file transaction  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.3 Add local prerequisite diagnostics  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.4 Generate local database and application environment files  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.5 Start and stop the generated application locally  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T7.7 Persist local status and safe cleanup inventory  Owner: L09  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T8.1 Freeze two selectable AWS reference profiles  Owner: L10  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T8.2 Define provider authentication and bootstrap contract  Owner: L10  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.
- [x] T9.1 Define generated CI fragments and trust boundaries  Owner: L12  Est: historical  kind: agent
  - Evidence: original accepted record in execution-state.json; no new retrospective stage or acceptance is asserted.

### E-SDLC-1 -- E1 delivery stages -> sdlc/E1.md (0/6)

### E-SDLC-2 -- E2 delivery stages -> sdlc/E2.md (0/24)

### E-SDLC-3 -- E3 delivery stages -> sdlc/E3.md (0/72)

### E-SDLC-4 -- E4 delivery stages -> sdlc/E4.md (0/60)

### E-SDLC-5 -- E5 delivery stages -> sdlc/E5.md (0/84)

### E-SDLC-6 -- E6 delivery stages -> sdlc/E6.md (0/78)

### E-SDLC-7 -- E7 delivery stages -> sdlc/E7.md (0/60)

### E-SDLC-8 -- E8 delivery stages -> sdlc/E8.md (6/156)

### E-SDLC-9 -- E9 delivery stages -> sdlc/E9.md (0/96)

### E-SDLC-10 -- E10 delivery stages -> sdlc/E10.md (9/99)

### E-SDLC-11 -- E11 delivery stages -> sdlc/E11.md (0/84)

### E-SDLC-12 -- E12 delivery stages -> sdlc/E12.md (0/66)

### E-SDLC-13 -- E13 delivery stages -> sdlc/E13.md (0/78)

### E-SDLC-14 -- E14 delivery stages -> sdlc/E14.md (0/84)

### E-SDLC-15 -- E15 delivery stages -> sdlc/E15.md (0/84)

### E-SDLC-16 -- E16 delivery stages -> sdlc/E16.md (0/72)

### E-SDLC-PROD -- Production delivery -> sdlc/production.md (3/15)

### E-SDLC-HOST -- Finite host routing -> sdlc/host-routing.md (6/6)

### E-SDLC-RUNTIME -- Runtime-only PostgreSQL -> sdlc/runtime-storage.md (3/9)

Validation: [obtained planning checks](sdlc-validation.md).
