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
