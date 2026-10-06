# Integrator follow-on assignments

These bounded assignments complete application composition for existing source tasks. They do not change frozen wire contracts or claim deployment qualification. The integrator retains module, migration ordering and execution-state ownership.

## INT-LOG-01: T10.3 runtime installation

- Owner: logging integration worker; independently reviewed by a separate agent.
- Prerequisite: reviewed telemetry component and trusted-ID runtime bridge landed; health-host integration is preserved.
- Owned paths: `app/app.go`, `internal/runtime/runtime.go`, and logging integration tests under `app/` and `internal/runtime/`.
- Deliverable: install structured logging once at the shared runtime boundary, forward an optional logger, and derive log templates from recognized fixed routes and registered business routes. Unmatched requests receive the bounded unmatched label. Raw URL paths and request secrets must not become log templates.
- Verification: scoped tests, race checks, vet and lint under shared capacity rules; tests cover automatic runtime installation, dynamic templates, unmatched paths, structured error correlation and request privacy. Independent exact-head review precedes merge and landed verification.
- Remaining acceptance: generated executable output and host configuration are checked separately; local source results do not qualify cloud providers or production.

## T10.7: consistent logical backup adapter

- Owner: backup source worker; independent review remains a distinct lane.
- Prerequisites: accepted operations, persistence and deployment-profile design components plus reviewed shared SDLC gates.
- Owned paths: `internal/backup/create.go` and `internal/backup/create_test.go` only.
- Design: exported repeatable-read snapshot ties a complete logical archive to the migration ledger. Caller pins the exact compatible tool version, supplies trusted module metadata and scoped credentials, and sets finite duration/size limits. Private staging and no-overwrite publication must never expose a failed or partial artifact as complete.
- Verification: real disposable PostgreSQL and pg_dump, interruption and collision negatives, restored positive checks, race/vet/lint and independent review.
- Boundaries: one database snapshot; no schedule/RPO, remote encrypted storage, production role, live-provider or restore qualification. These remain separate tasks.
- Documentation: state mandatory size limits and that one logical snapshot proves no schedule or RPO target. Managed automated backups/PITR complement it; VM volume snapshots are crash-consistent complements, never replacements for a verified logical backup.

## T2.8: operation contract preflight

- Owner: integrator; independent security and compatibility review is a separate lane.
- Status: proposed ADR 023 and invocation contract; runtime dispatch remains held.
- Scope: reconcile frozen `policy/` with task `app/policy/`, the typed extension callback, current identity/workspace/session authority, durable replay storage and finite audit changes.
- Source prerequisite: adopt the reviewed amendment, update task ownership and acceptance together, then explicitly assign each source slice. Proposal text grants no source or migration ownership to a worker.
- Verification: independent privacy/security and compatibility review against current source and contracts; plan/public-artifact checks. Real database and transport tests remain required for implementation acceptance.
