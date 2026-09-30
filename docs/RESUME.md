# Current implementation checkpoint

Implementation is authorized and underway. Original AMOS code uses Apache 2.0. All Pulumi programs use Go. The owner has granted full command access and autonomous work; honor that grant and the actual runtime permissions without repeated confirmation questions.

Read `docs/planning/execution-state.json` before dispatch; it is the authoritative accepted-task registry. Integrated code includes the Go module, real PostgreSQL harness, migration ledger, durable jobs, identity storage, opaque browser sessions, bounded Argon2id password verification, protected SES delivery boundary, typed OpenAPI generation, runtime routing and reviewed workspace/billing/UI contracts. Workspace persistence and audit sink are under integration review. The most recent integrated lint and tagged database suite passed before the latest password normalization-only cleanup; run appropriate current checks after changes. No runnable complete SaaS app, live-provider qualification or production release is claimed.

Current parallel coding lanes implement the durable reference business app, workspace admission/concurrency corrections and tenant-scoped audit sink. Password verification uses a fixed legacy/current work schedule and process-wide admission cap. Owner-supplied blocklist/rate-limit adapters and deployment benchmarks remain release gates. Email outbox material references are canonical opaque UUIDv7 identifiers; owner protected-material resolution and live SES remain staged. Resume the dependency-ready tasks rather than re-planning the architecture.

Use isolated task worktrees and atomic task claims. Shared builds require the build lease and load check. Recover failed worker turns explicitly; do not assume a running model survives connection failure. Three inexpensive-model workers plus an integrator is the current harness capacity. Preserve existing work and do not certify future controls from document validators.

The historical planning handoff below explains architecture and full inventory; its planning-only status is superseded by this checkpoint.

# Resume AMOS work

This is a public, self-contained handoff. Product implementation has not been claimed by the planning artifacts.

## Read in order

1. `docs/VISION.md`: confirmed direction, staged scope and remaining choices.
2. `docs/rfc/rfc-0001.md`: architecture and explicitly proposed defaults.
3. `docs/adr/`: decisions and their status, including independent agent runtimes and selectable AWS profiles.
4. `docs/plan.md`: stages, dependencies, ownership, release gates and current execution state.
5. `docs/planning/contracts.md`: cross-lane contract version and shared-file owner.
6. The assigned `docs/tasks/<id>.md`, its dependencies, and relevant public research/evidence.

## Do not lose these distinctions

- AMOS delivers a complete application shell plus business extension points, not only a hosted identity service.
- One public domain serves both shared pages and business routes; an optional internal service stays private.
- Independent agents and Zatiti connect to standard generated MCP; AMOS does not host them or duplicate their customer-governance UI.
- Application auth/permissions/entitlements remain mandatory on every interface. Agent access does not bypass proof requirements.
- Personal and organization billing are separate; all three pricing models and the selected full authentication suite remain planned even when an early release is narrower.
- Owners can replace every UI surface. Upgrades must preserve custom work and stop on incompatible contracts.
- Maintenance automation is a distinct owner-controlled development/deployment system with protected verification and release authority.
- Private business fixes stay private. Public upstream diagnostics are allowlisted and sanitized; raw logs and sensitive source context never become default public issues.
- Both AWS profiles are selected per installation, with separate operational guarantees.
- AMSL is a candidate reusable foundation requiring qualification; application policy and composition stay in AMOS. Upstream maturity/review rules are not bypassed.
- A complete future task inventory is not evidence of implementation or cheap-tier execution certification.

## Before dispatch

Read repository instructions, current Git status and ownership. Preserve other work. Run the plan validator; resolve the task's provisional contracts, exact dependencies and external gates. Select an isolated worktree and claim its task using the approved mechanism. Shared module/schema/API/entrypoint/workflow changes need the integrator's exclusive ownership.

Use actual available concurrency up to the plan ceiling. Build-resource leases and runner trust take precedence over worker count. Never run sixteen heavy builds on one machine because the plan allows sixteen coding lanes.

If a planned task is already implemented, materially underspecified or blocked, report the evidence and update the plan before proceeding. Keep implementation, integrated checks, provider acceptance, deployed behavior and release certification distinct. No remaining application behavior should be inferred from a document title or passing mock.

## Publication boundary

Keep this repository public-safe: no private project references, home paths, private source hashes, infrastructure account identifiers, customer/attendee data, secrets or internal schedules. Use public independently reproducible evidence and synthetic fixtures. Planning grants no provider spending, publication or deployment authority.
