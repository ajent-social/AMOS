# AMOS implementation plan

<!-- current-native-projection:start -->
## Current read-only display projection

Product acceptance: **59/257**. Combined inventory: **1598 product and delivery nodes**; node completion is not product completion.
This current view supersedes historical inventory/tooling statements below; the original 1,501 IDs remain retained.
Checkboxes report registry status only; they grant no execution, acceptance or release authority.
The consumer may additionally display dependency-blocked status. Consult the registries and claims before dispatch.
Product `S0`-`S5` milestones remain `product-milestone` metadata; their display `Stage: implement` is a work bucket, not advancement.
Delivery aliases: `author` -> `implement`; `landed` and `accept` -> `verify-landed`. Original labels remain `authored-stage`.
Unrecorded tasks remain unchecked. Checked product rows report ACCEPTED; checked delivery rows report COMPLETE.
These generated fields do not modify canonical authored statuses or authenticate registry receipts.
Canonical source: `docs/planning/wazi-source.json`; `sha256:5e2cb731acd3ba85e8f4a42cc41d14a24c0a00cbac680c8365728895497491e0`.
Registry: `docs/planning/execution-state.json`; `sha256:e25d22caec19ab9b4a2f60767d892f2eb40c14dcd1d2dccc2ee25d34cfd81842`.
Registry: `docs/planning/sdlc-stage-state.json`; `sha256:6ae4b0594209343bcb0f27305da70a96d562807833073626eba0d21932269004`.
See [projection contract](planning/portable-plan-export.md) for regeneration and limits.
<!-- current-native-projection:end -->

## Wazi-compatible authored plan migration

The complete authored task graph now lives in
[wazi-source.json](planning/wazi-source.json): all 257 original product tasks and
1,244 delivery-stage tasks, with stable IDs, acceptance wording, original product
records, lifecycle records and historical narrative status preserved. This
section supersedes earlier source-location instructions below; the remaining
document retains the original scope and history.

Produce the Wazi 0.0.1 interchange with: go run ./cmd/portableplan --sdlc
The result contains one source-bound definition with 1,501 tasks. It is a
read-only view, not a second scheduling authority. All prerequisites referencing product tasks retain
domain-acceptance semantics; genuinely untyped lifecycle-to-lifecycle ordering remains
execution-complete. Stage labels alone do not establish review or deployment
approval. No execution snapshot, qualified evidence or satisfied evaluation is
manufactured from narrative completion.

After this migration is adopted, edit the combined authored source. The older
plan-data.json and sdlc-plan.json are retained migration baselines and must
not be independently advanced as competing plan masters. Retired render/check/release commands refuse current-mode invocation to prevent
stale views or qualification. Their replacement remains a tooling gate; the
exporter checks graph/preservation invariants and the Wazi owning validator
checks portable coherence. The default export now selects the complete graph;
--historical-product explicitly selects a distinctly identified retired baseline. Execution evidence remains in its existing
registries and receipts, separately from authored intent. Before new dispatch,
reconcile those live records and claims; this migration grants no admission.

See [mapping and validation](planning/wazi-migration.md) and
[recovery disposition](planning/worktree-recovery.md).

## 2026-10-04 SDLC and production refinement

The current accepted registry preserves **59 accepted product tasks of 257**. The dated initial inventory description below is historical. This refinement adds **1,200 first-class delivery-stage tasks for the 200 unaccepted product tasks**, plus **15 shared/production gates**, without changing original IDs, task contracts or obtained acceptance evidence.

The delivery endpoint is now explicitly **the entire scoped product running in production at https://amos.sire.run on AWS**. See the [SDLC operating contract and task index](planning/sdlc-delivery.md), [local draft dependency graph](planning/sdlc-plan.json), [production stage tasks](planning/sdlc/production.md) and [delivery decision](adr/022-first-class-sdlc-production-plan.md). These supplement the existing split epics. Product milestones and full release T16.12 are intermediate; terminal completion requires `T-PROD.13` after live production and operating verification.

Use maximum eligible GPT-6-Luna parallelism within actual runtime slots and the existing 16-lane ceiling, keeping independent reviews, integrator ownership and shared build limits. Continue dependency-ready waves through release, AWS deployment and final operating acceptance during `/ship`; this `/plan` refinement does not execute them.


Date: 2026 10 03. Status: complete initial task inventory; implementation underway. This is an engineering, documentation and operations plan. It preserves the full product architecture while delivering independently qualified stages. Current acceptance is 59 of 257 tasks per the execution registry; this count is not a product-completion measure.

## Context

AMOS supplies a complete open-source application foundation: owner-hosted web application, identity/workspaces/policy/audit/billing, developer initialization/deployment CLI, business extension routing, OpenAPI-derived APIs/MCP, customizable UI, upgrades, operational tooling and bounded domain maintenance. AMOS remains useful during external workflow outages and does not duplicate task/PR orchestration. See [VISION](VISION.md), [RFC 0001](rfc/rfc-0001.md), [RFC 0002](rfc/rfc-0002.md) and the [decision records](adr/001-application-boundary.md).

Default development stack is Go SSR, HTMX and plain JavaScript/CSS. Owners select either managed containers/managed PostgreSQL or a smaller VM running the app and PostgreSQL. Both profiles use AWS and required Cloudflare. External agents run independently. Personal and organization subscriptions remain separate. All selected authentication and pricing models are in scope even when an early staged release is narrower.

The implementation plan contains **257 tasks across 16 epics**, **116 planned use cases**, and **16 ownership lanes**. The dependency/ownership scheduler yields **48 waves with a peak of 12 simultaneously eligible task slots**. The dispatch ceiling is 16, not a requirement to manufacture parallel work or run 16 builds. Actual runtime capacity and external gates can reduce concurrency.

Estimates sum to **342.75 task-hours** across the complete inventory. These are planning hypotheses for bounded tasks, not measured agent throughput, calendar commitments or provider onboarding estimates. Re-split any task that exceeds its boundary after contract freeze.

### Constraints and assumptions

- Full architecture and later task contracts are written now by explicit project direction. This overrides rolling-wave outline-only planning. Later contracts remain provisional until stage revalidation; no inexpensive-agent certification is claimed.
- Keep every repository artifact public-safe. No restricted source identifiers, home paths, customer data, infrastructure account details, credentials or private scheduling context.
- Prefer qualified AMSL mechanisms and mature maintained libraries. No hypothetical shared framework or new cryptographic/payment/MCP protocol implementation.
- Provider onboarding, paid model/cloud access, publication rights and deployment authority are external gates. Mocked success never substitutes for them.
- Cross-cutting decisions belong to the integrator and explicit freeze tasks. Current Git/source state must be inspected before dispatch.
- The original product license, exact provider/toolchain profiles and financial/recovery policy require named decisions. Proposed defaults do not silently become owner approvals.

### Success measures

Measure fresh-repository setup/rehearsal time, manual interventions, declared versus observed capability support, denied-path correctness, recovery observations, upgrade conflict rate and maintenance actions stopped by policy. Do not invent targets already achieved. The owner can explain account/cost/credential responsibilities and reproduce the first useful business change.

## Discovery Summary

The AMOS tree initially contains planning inputs, not a qualified product. Public AMSL contracts/source expose useful candidate mechanisms and known composition gaps. Graph architecture probes supplied no populated communities, so direct source inspection was used. Specialist source inspection did not execute package suites, providers or cloud deployments.

Three Astra specialist lanes produced identity/billing/UI, platform/operations/upgrades and API/MCP/maintenance/security inventories. Independent architecture/security reviews found and corrected shared-policy ownership, baseline subscription management, stale deployment approvals and restore-authority barriers. The integrator resolved semantic dependencies and verified the combined graph.

Public source findings: [AMSL boundary](planning/amsl-boundary.md), [identity/billing research](planning/research/identity-billing.md), [platform research](planning/research/platform.md), [agent/security research](planning/research/agents-maintenance.md), [review resolutions](planning/review-resolutions.md).

## Use Case Summary

All **116 use cases are PLANNED**. There is no implementation coverage implied by task linkage. The [readable catalog](use-cases.md) maps every use case to implementation tasks. The canonical [JSON manifest](usecases-manifest.json) records actors, prerequisites, proposed interfaces and outcomes; a local scratch copy supports planning tools.

## Scope and Deliverables

| Deliverable | Owner | Observable acceptance |
|---|---|---|
| Versioned contracts and public provenance | L01/L02 | Resolved principal/policy/routing/storage/version seams, candidate reuse evidence and allowed dependencies |
| Local generated paid app | L02-L09 | Durable business feature, baseline verified identity, personal workspace, flat paid access and billing management through real app boundaries |
| Both AWS deployment profiles | L10/L11 | Independent deploy/isolation/backup/recovery evidence and truthful differences in availability/maintenance |
| Full identity, organizations and billing | L03-L08 | Selected methods, MFA/OIDC, membership lifecycle and flat/seat/usage policies verified at actual enforcement boundaries |
| Complete API/MCP and separate service support | L02/L14 | Generated contract mapping, current-state access and real-client/proxy denial evidence |
| Upgrades and operating tooling | L09-L13 | Reviewed upgrade PRs preserve custom work; restore/rollback and operational limits demonstrated |
| Private and upstream autonomous maintenance | L15/L16 | Protected code/verifier/release identities, sanitized reporting, bounded eligible promotion and independent suspension |
| Qualified staged releases | L01 plus reviewers | Exact artifact evidence for every advertised capability, public-safe handoff and supported-version matrix |

Out of scope for this plan: GCP implementation, SAML, end-user native/mobile/SMS/CLI clients, a new agent runtime, mandatory hosted AMOS service, broad customer-agent governance UI, generalized WorkOS feature parity, and unspecified future agent protocols. These are not required to satisfy the selected first full product scope. Directory synchronization or unrelated advanced vendor-product features are not inferred from historical brainstorming.

## Checkable Work Breakdown

Every epic below links to checkbox tasks and per-task contracts. The generated epic checkbox totals are the initial inventory snapshot and are not current acceptance status; consult the authoritative execution registry. `fidelity: executable` indicates detailed task format, not permission/readiness/certification; external gates and unmet dependencies remain binding.

### E1 -- Foundation contracts provenance and verification harness -> docs/plans/E1.md (0/13)

[Open E1](plans/E1.md). Create stable seams and honest evidence before domain fan-out.

### E2 -- Contract generator runtime composition and reference business app -> docs/plans/E2.md (0/9)

[Open E2](plans/E2.md). Prove a useful integrated application path from contract to paid feature.

### E3 -- Identity, authentication and account security -> docs/plans/E3.md (0/23)

[Open E3](plans/E3.md). Compose a durable person identity and all founder-selected authentication methods; AMOS owns sessions, lifecycle and assurance while qualified AMSL mechanisms remain narrow.

### E4 -- Personal and organization workspaces, roles and lifecycle -> docs/plans/E4.md (0/14)

[Open E4](plans/E4.md). Keep tenant selection and membership authoritative at each resource boundary, with independent workspace ownership and billing.

### E5 -- Independent subscriptions, seats and usage billing -> docs/plans/E5.md (0/22)

[Open E5](plans/E5.md). Compose Stripe-first durable billing behind explicit provider capabilities; keep entitlement policy, tenant billing ownership and financial event identity in AMOS.

### E6 -- Default shared UI and fully replaceable client contracts -> docs/plans/E6.md (0/18)

[Open E6](plans/E6.md). Deliver server-rendered Go/HTMX lifecycle pages whose behavior comes from public domain/API contracts; owners can theme, replace a page or supply a wholly custom UI.

### E7 -- Initializer and local/deployment CLI lifecycle -> docs/plans/E7.md (0/16)

[Open E7](plans/E7.md). Deliver a deterministic local application path first, then explicit cloud planning and deployment commands. Root-owned CLI registration and shared schemas are integration seams, not lane-owned files.

### E8 -- AWS and required Cloudflare provisioning -> docs/plans/E8.md (0/28)

[Open E8](plans/E8.md). Support two explicit installation profiles: managed ECS Fargate plus private managed PostgreSQL, and a small EC2 VM running the app plus owner-hosted PostgreSQL. Qualify the managed profile first in S2, then the small-VM profile in S3/S4. Cloudflare is required in both. Each profile has independent cost, availability, credentials, backup, deployment and live-evidence contracts; switching profiles is an explicit data migration.

### E9 -- CI, release artifacts and versioned application upgrades -> docs/plans/E9.md (0/17)

[Open E9](plans/E9.md). Maintain packages, workflows and generator versions centrally while application owners retain business code and overrides. Ship local validation and ownership metadata early; upgrade automation later must produce reviewable changes, never silently overwrite custom code or merge itself.

### E10 -- Observability, backup, recovery and operations -> docs/plans/E10.md (0/17)

[Open E10](plans/E10.md). Make health and safe diagnostics part of the first local reference; qualify durable backups, isolated restore, compatible rollback and ongoing operations before operational readiness claims. Availability, recovery targets and retention are owner-approved values, not invented guarantees.

### E11 -- REST/OpenAPI-to-MCP access and bounded credentials -> docs/plans/E11.md (0/14)

[Open E11](plans/E11.md). Expose applicable application behavior to independent clients through standard MCP and ordinary application authority.

### E12 -- Contract-first business extensions and custom-client conformance -> docs/plans/E12.md (0/12)

[Open E12](plans/E12.md). Support integrated Go and private separate-service business implementations under the same public API/security boundary.

### E13 -- Private owner-run autonomous maintenance -> docs/plans/E13.md (0/13)

[Open E13](plans/E13.md). Separate observation, coding, verification and release in owner infrastructure with owner credentials and explicit bounded automatic authority.

### E14 -- Sanitized diagnostics and upstream autonomous repair -> docs/plans/E14.md (0/14)

[Open E14](plans/E14.md). Separate owner-private repair from publishable AMOS/AMSL improvement using bounded diagnostics and independent upstream controls.

### E15 -- Security, supply chain and recovery qualification -> docs/plans/E15.md (0/15)

[Open E15](plans/E15.md). Turn application and autonomous-maintenance threat boundaries into protected controls and executable negative tests.

### E16 -- Composed qualification release and operational acceptance -> docs/plans/E16.md (0/12)

[Open E16](plans/E16.md). Qualify composed behavior and live claims separately from package completion.

## Parallel Work

The [execution guide](planning/execution.md) defines all 16 ownership lanes, integrator-only shared files, review rules and build limits. The [wave inventory](planning/waves.md) gives exact task slots, lane and stage for each dependency-ready wave. It enforces one active task per lane and avoids overlapping owned path prefixes within a wave.

Wave assignment is an earliest eligible grouping across independent stages, not blanket stage approval. Resolve a stage's entry gate before dispatching its tasks. If an external gate blocks a task, its descendants remain blocked while other ready work can continue. Current session limits never get bypassed to match the plan ceiling.

## Timeline and Milestones

| Milestone | Stage | Task inventory | Exit gate |
|---|---|---:|---|
| M0: contracts and harness | S0 | 9 | T1.1-T1.9 accepted; cross-lane freeze and first intended-tier contract trial documented |
| M1: local paid reference | S1 | 61 | T16.2; meaningful paid/denied/revoked behavior, account/billing management and clean-repo rehearsal |
| M2: managed cloud baseline | S2 | 32 | T16.3-T16.5 plus relevant ingress/artifact/security gates; real provider test-mode and authorized cloud evidence |
| M3: complete application capabilities | S3 | 89 | T16.6-T16.8; all selected identity/organization/billing/agent interfaces and secondary-profile implementation |
| M4: upgrades and complete operations | S4 | 39 | T16.9, T8.25-T8.26 and operational/security exits; both profiles independently qualified, explicit profile migration, restore and override compatibility |
| M5: bounded autonomous lifecycle | S5 | 27 | T16.10-T16.12; private/upstream repair, privacy/adversarial evidence and every remaining terminal branch accepted |

The minimal dependency closure for T16.2 is 53 tasks / 64.75 estimated task-hours, with an ideal dependency-only lower bound of 19.75 hours. This omits review delays, lane/resource contention, revisions, provider onboarding and human gates. It is **not a completion forecast**. Build and rehearse the local slice first; do not broaden advertised scope to cover unfinished provider/cloud work. Generic reference data supplies a fallback when external setup is blocked.

A calendar-specific delivery commitment needs current capacity, actual execution throughput and owner-provider readiness; none is established by this document. Preserve the complete target while selecting independently useful staged artifacts.

## Risk Register

| Risk | Impact | Mitigation / owning tasks |
|---|---|---|
| Candidate library semantics mistaken for complete platform | Wrong identity/billing guarantees | T1.4, T3.22, T11.8, T14.10 and composed E16 gates |
| Shared-model or migration conflicts across workers | Rework or unsafe persistent state | T1.2/T1.3, integrator ownership, per-wave path checks |
| Provider/sender/domain onboarding delay | Real journey cannot qualify | T8.27/T8.28, T3.21/T5.10/T5.20; local fixtures labelled separately |
| Open source contains restricted source or operational context | Disclosure and redistribution failure | T1.4/T1.6/T1.7; public-safe evidence; no blanket source copying |
| Cross-tenant or proof/entitlement bypass | Unauthorized application access | T2.8, E3-E5, T11.13, E15 adversarial checks |
| Usage replay, stale payment projection or seat races | Incorrect charges/access | T5.7-T5.9, T5.13-T5.20 |
| Upgrades overwrite custom code or break UI contract | Downstream application loss | T9.8-T9.16, T6.17 and T16.9 |
| Backup restores revoked authority | Security regression | T3.23, T10.10/T10.11, T15.13; fail-closed activation barrier |
| Older approved repair overwrites newer release | Invalid autonomous deployment | T13.10 target-generation CAS, current-policy recheck, T15.14 |
| Malicious logs/PRs influence privileged agents | Exfiltration or unauthorized promotion | E13-E15 separation, untrusted input handling, bounded egress and immutable gates |
| Two AWS profiles multiply operating burden | Unqualified assumptions about resilience | Independent T8.17/T8.26 and profile-specific recovery evidence |
| Task estimates or low-tier contracts fail in execution | Missed expectations and wrong software | T1.9 certification trial, stage revalidation, feed failures back into contracts |

## Operating Procedure

Follow [execution.md](planning/execution.md). An implementation task needs meaningful scoped checks, format/lint, reviewed integration and explicit limitations. A release additionally needs actual provider/live verification where applicable, with authorized resources and artifact identity. Missing production infrastructure is reported as absent; staging/fixtures cannot be relabelled production.

A new worker starts from [RESUME](RESUME.md), reads its task and completed dependencies, claims ownership, and uses an isolated worktree. A single integrator updates this plan. Re-render after reviewed changes to canonical planning data and run `python3 scripts/check-plan.py`. Never promote a task to execution-certified without the intended cheap-tier run and reviewed evidence.

### Accepted delivery and review boundary (2026-10-03)

The immutable approved product plan remains the source for product scope and aggregate admission. An enrolled code-change lifecycle has one canonical scheduler and authoritative admission/result adapter. It exposes executable author, independent review, bounded fix, re-review and delivery rows through ordinary plan/apply/claim; a projection or checkbox alone is not a task acceptance. Coding finishes at PR URL and exact-head handoff. Every code-changing PR receives independent exact-head review. A blocking finding creates visible bounded fix and re-review rows; attempted negative review does not release the delivery gate. Ordinary descendants wait for reviewed merge and verified landing. Explicit speculative dependencies may start earlier, without release authority. No task acceptance is inferred or changed here.

AMOS owns identity, workspaces, policy, audit, billing and API/MCP. The code-change lifecycle service owns PR safety and its executable child tasks. The product workflow controller owns delivery, qualification and product-level dependencies. Shared generic plan/apply/claim representation, readiness and stage routing remain reusable; an enrolled lifecycle uses its service's authoritative admission/result adapter. AMOS retains owner-local diagnosis, proposal/upgrade generation and bounded domain jobs, and remains useful if either external service is unavailable. Do not build a second task/PR orchestrator in AMOS. Release and upgrade profiles are immutable and versioned; restricted source is not transplanted into this public repository.

The execution record remains authoritative for accepted tasks. This documentation reconciliation does not modify its records or infer acceptance from local generation, billing components, a design artifact or external-service contracts.

Runtime-only PostgreSQL is an additive composition assignment under v1.15. Its
[six delivery stages](planning/sdlc/runtime-storage.md) preserve legacy development
APIs and gate complete-product reconciliation. Design delivery does not accept
source implementation, production database grants or deployment.

## Progress Log

2026 09 30: created public VISION/RFC, ADRs 001-008, E1-E16 full inventory, 257 task contracts, 116 use cases, resolved dependency waves and review corrections; implementation and provider qualification remained not started at that time. This is historical; later acceptance evidence is recorded only in the execution registry.

2026 10 03: reconciled accepted external lifecycle boundaries and current public status. No execution-state record changed; no new task acceptance, provider qualification or production readiness is claimed.

## Hand off Notes

- Start at [RESUME](RESUME.md); confirmed direction is not the old brainstorming proposal.
- [plan-data.json](planning/plan-data.json) is the canonical structured inventory after integration; specialist inputs preserve domain drafts and findings. Generated task/epic files must stay synchronized.
- Exact command names in task contracts are planned acceptance entrypoints to implement. Successful command exit with no tests, skipped required services or fixture-only provider checks is not acceptance.
- [Open decisions](planning/open-decisions.md) names the freeze task and proposed default for unresolved choices. Do not improvise commercial or security policy.
- Upstream changes are separate repository tasks with their own provenance/review/release authority. Sibling working trees are not implicitly writable.
- Initial task certification is NOT_RUN. The current plan passes structural checks, not software or live-service tests.

## Appendix

[Requirement coverage](planning/requirements.md), [AMSL strategy](planning/amsl-boundary.md), [contracts](planning/contracts.md), [review resolutions](planning/review-resolutions.md), [roadmap](roadmap.md) and [development log](devlog.md).
