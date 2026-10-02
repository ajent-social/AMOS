# Current implementation checkpoint

Updated: 2026-10-01. Reviewed foundations and application components are integrated; implementation remains underway. Original AMOS code uses Apache 2.0. Application code and all Pulumi programs use Go. Existing task permission grants remain in effect; routine authorized work does not require repeated confirmation.

Read `docs/planning/execution-state.json` before dispatch. It is the authoritative accepted-task registry. At this checkpoint, 52 tasks are accepted out of 257 planned. Reviewed components include initializer transactions, credential authentication and recovery, billing persistence/catalog/Stripe adapter/checkout controllers, reference UI, local database runner and cloud configuration/profile designs. Generated application composition, complete auth and organization policy, live subscriptions and deployment remain in progress. Do not assume that a task worktree is merged or accepted merely because it exists.

Integrated foundations cover PostgreSQL/migrations, policy/configuration, jobs/outbox/email/audit, identity storage/sessions/passwords/email verification, workspace persistence/bootstrap/context, OpenAPI/Go generation, tenant-scoped reference todos and the default UI renderer. The fragment swap regression failed before the renderer fix and passes afterward in a real browser.

AMOS is not yet a complete runnable SaaS. Generated application and executable wiring, full identity and organization flows, subscription processing, cloud deployment, generated MCP, upgrades and operating recovery remain incomplete. No live provider or production deployment qualification is claimed.

Prioritize a generated runnable app with signup, verification, sign-in, a personal workspace and the reference business feature; follow with billing and qualified deployment. Preserve the complete later architecture in the staged plan rather than narrowing the product vision to this first slice.

Use isolated task worktrees and atomic task claims. Shared multi-package builds require the build lease and load check. Record only obtained evidence, preserve existing work, and keep public artifacts free of private operational details. Recheck Git status, worker ownership and prerequisites at every lane boundary.

The handoff below preserves architecture and dispatch rules; the checkpoint and execution record govern current implementation status.

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
- Independent agents and external runtimes connect to standard generated MCP; AMOS does not host them or duplicate their customer-governance UI.
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

### Historical pending checkpoint (superseded by acceptance records below)

- Main remains at the reviewed 46-task checkpoint before the current two service acceptances; additional source is staged
  in the integration branch and has not been accepted merely because it builds.
- Magic-link and verified webhook ingress scoped tests passed against the
  required PostgreSQL environment after integration. The webhook transaction
  now includes a durable reconciliation job, including quarantined receipts.
  Subscription projection and live provider qualification remain separate.
- Independent headless review cleared the CI, operations configuration and
  business contract schemas as design artifacts only. Scoped schema tests passed;
  this does not demonstrate executable workflows or operational recovery.
- The local runtime composition reached signup, captured verification,
  sign-in, personal todo creation and sign-out in its HTTP test; cleanup failed
  and the test must pass again after the shutdown correction. Review found a
  foreign quarantined receipt database-binding gap; a fix and regression test
  are pending verification. The executable generated application is unfinished.
- Authentication UI, TOTP and concrete local application generation are active
  isolated lanes. Durable session assurance and factor admission seams are
  pending independent review and actual tests. Shared machine build leases may
  delay verification; a held or lost lease is not a passing test.

## Current service acceptance

Magic-link service (T3.9) and durable signed webhook ingress (T5.7) now have
independent review, worker negative tests, root real PostgreSQL tests and root
zero-issue scoped lint. They are accepted as service components. Generated native
application browser qualification remains pending: signup, explicit email
confirmation and password sign-in progressed in Chromium; reset replay browser
coverage is still being corrected. Runtime startup private-directory protection
was strengthened after review. No live mail, Stripe or cloud deployment claim.

## TOTP service acceptance

T3.15 is accepted as an independently reviewed service component. The isolated
service batch passes fresh PostgreSQL race tests, vet and zero-issue lint; replay,
statement-clock and guarded-cookie admission regressions have actual negative
evidence and restored passing checks. Native application/UI wiring remains a
separate staged batch. Production key management, organization/enterprise policy,
live provider qualification and deployment are not established.

## Native password UI acceptance

T6.3 is accepted as an independently reviewed UI component, with fresh race,
vet, lint and repeated real PostgreSQL/generated executable/Chromium evidence.
The reset replay case follows Back navigation and resubmits a reconstructed
previously rendered form; it does not claim history restored that form itself.
Native host/generator shipping remains a separate batch. No live provider,
subscription, organization, cloud or complete release qualification is implied.

## Native host acceptance

The local personal application host is reviewed and accepted as a composition
component. Fresh real PostgreSQL race tests, vet and zero-issue lint pass in the
isolated batch; shutdown, immutable database binding and finite guarded MFA
checks described as pending in the historical checkpoint now pass. See
[evidence](evidence/native-host-20261001.md). Generator and CLI shipping remain a
separate batch; full SaaS, live providers and cloud deployment are not qualified.

## Native generator acceptance

The config-driven CLI now generates a runnable Go personal evaluation app.
Independent follow-up review cleared port preflight and terminal-control
corrections; fresh race, vet, lint, actual generated executable/PostgreSQL/Chromium
and three negative/restored checks pass. See the README commands and
[evidence](evidence/native-generator-20261001.md). Current Compose provider absence
remains explicit; no live providers, organization policy, production targets or
cloud deployment are qualified. This extends accepted initializer/local scaffold
components without accepting an entire SaaS or narrowing the complete plan.

## Billing reconciliation components

T5.8 remains in progress after the isolated core/official SDK component batch.
Fresh real PostgreSQL race tests, vet and zero-issue lint pass; expired retry
leases were corrected after independent review with genuine negative/restored
evidence. See [checks and limits](evidence/billing-reconciliation-20261001.md).
Durable consumer/executable scheduling, shared migration allocation, account-wide
throttling and complete subscription/access policy remain separate; live Stripe
credentials are absent and fixtures are not provider qualification.

## Workspace UI component

The reviewed workspace switch UI component passes fresh scoped race, vet and
zero-issue lint. Stale hints are cleared and current authorized choices are
queried again; error DTO fields never render. T6.4 remains in progress because
the composed organization browser fixture and native adapter are absent. See
[evidence](evidence/workspace-switch-20261001.md).

## Native development supervision component

The reviewed local runner starts the generated native application with real
Podman PostgreSQL and does not require Compose. Installation identity and
process state are pinned to private ownership; Record/Clear updates use a
persistent lock. T7.7 remains in progress: clean --plan is advisory only and
full cleanup execution remains unqualified. See
[evidence](evidence/native-dev-supervision-20261001.md).
## Durable billing webhook job component

The independently reviewed durable billing-only consumer now links verified
receipts to reconciliation work, with database-clock lease guards after lock
waits. Duplicate receipts preserve active work leases and dirty generation.
T5.8 remains in progress; executable scheduling and live Stripe qualification
remain separate. See [evidence](evidence/billing-webhook-jobs-20261001.md).

## Inflight integration audit

The already integrated CI trust and operations target design contracts are now
accepted as T9.1 and T10.1 after fresh checks and negative/restored replay.
The business-extension schema participates in the root contract checker; T12.1
remains in progress at its shared API, wire-code, path and policy-schema gates.
Older lane commits are not blanket merge candidates: native/billing components
and the revised RFC are already integrated, and old trees contain stale copies.
No existing lane worktree was reset or cleaned. See the
[integration audit](evidence/inflight-integration-20261001.md) for current checks
and remaining implementation gates.
