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

## T2.8: operation invocation implementation gates

- Owner: integrator; independent security and compatibility review is a separate lane.
- Status: v1.12 design landed; v1.13 explicit codec schema binding becomes frozen after independent exact-head review and merge. Runtime dispatch and source acceptance remain held.
- Initial source slice: T2.8 worker may implement only immutable registry construction and typed descriptor validation under `app/operation/**`, consuming frozen policy contract types. No `app/policy/` path is owned by the worker.
- Root-owned integration assignments: policy implementation/adapters, transactional current-authority and session recheck seam, invocation SQL storage, finite audit API/schema, migration content/sequence/registry, and executable composition. Migration 17 stays a candidate until actually allocated.
- Runtime prerequisite: all adapters must implement the contract lock order or fail closed before dispatch; supported compositions must include the allocated migration. Exact replay must return the original validated result only after fresh authority checks; changed input/revision must conflict without callback. Required real PostgreSQL transactions prove scope isolation, revocation ordering, replay, audit/outbox/mutation atomic rollback, and explicit capacity exhaustion/increase.
- Verification: independent review against current source and contracts; plan/public-artifact checks for this design bundle. The implementation gate additionally requires scoped registry tests, required real database transaction tests and qualified transport integration. No fixture, design, or local unit check substitutes for those gates.

## INT-HOST-02: external consumer protocol and runtime preflight

- Owner: Coordinator; a distinct read-only reviewer must review the preflight. This assignment does not authorize dispatch or source changes.
- Scope: define finite versioned protocol-handler slots for the eight exact MCP/OAuth routes; define a method-neutral explicit business-route set with no root wildcard or reserved fallback; specify a production HTTPS/cookie/proxy constructor and a runtime-only PostgreSQL opener that receives no migration credentials and performs no DDL. Keep the requirements public-safe and consumer-neutral.
- Required evidence for a later source assignment: exact route/method/auth equivalence and non-alias/reserved-route rejection; real native-session deployment scope and current-proof behavior; verified TLS, rejection of ambient PostgreSQL configuration, least-privilege pool readiness and close behavior.
- Boundary: this preflight does not adopt an alternate identity model or infer production readiness. Source ownership follows only after contract review.

## T8.4: AWS network and workload identity source preflight

- Owner: integrator; the source component requires an independent exact-head review.
- Frozen profile: single-AZ public EC2 with an Elastic IP and direct TLS; no NAT, load balancer, public database, or SSH. Workload identity is limited to exact-scope host IAM and SSM.
- Worker-owned paths: `internal/infra/aws/network.go`, `internal/infra/aws/network_test.go`, `internal/infra/aws/identity.go`, and `internal/infra/aws/identity_test.go` only. The integrator owns `go.mod`/`go.sum` and pins Pulumi SDK v3.267.0 and AWS provider SDK v7.48.0 from official Go module metadata.
- Verification boundary: isolated Pulumi mocks prove source topology and least-privilege IAM negatives only. Provider credential bridge and actual infrastructure execution remain separate gates; no deployment permission or production claim is granted.
