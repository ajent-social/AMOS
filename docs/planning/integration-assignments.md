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


### INT-HOST-02 routing implementation slice

- Owner: assigned routing worker; independent review by another agent. Dispatch only after v1.14 design merge and an exclusive claim.
- Frozen contract: [finite host routing](../contracts/host-routing.md), ADR 025. The coordinator adopts exported aliases/options and delegates this bounded implementation.
- Owned paths: `app/app.go`, new routing tests under `app/`, `internal/runtime/runtime.go`, and new routing implementation/tests under `internal/runtime/`. Existing unrelated tests may change only for explicit late-registration compatibility cases after coordinator review.
- Required behavior: all-or-none eight protocol slots, explicit method-neutral business manifest, strict aliases/reserved boundaries, copied routing configuration, legacy compatibility, shared telemetry matching and permanent atomic registration freeze.
- Verification: contract route/method/auth-equivalence matrix, actual local HTTP boundary checks, deterministic race tests, genuine negative/restored checks, scoped race/vet/lint, independent exact-head review, guarded rebase merge and landed verification.
- Stage chain: `T-INT-HOST-02.1` through `.6` in the SDLC graph; complete product reconciliation depends on `.6`. No original accepted task is reopened or falsely reaccepted by this additive composition work.
- Deferred stages: runtime-only PostgreSQL, least-privilege role/TLS checks, production origin/cookies/proxy composition and deployed protocol acceptance remain unqualified. Routing may proceed independently; it does not enable the gated operation executor.

### T8.4 storage follow-up constraint

The host backup KMS policy scopes the encryption context to the installation and
environment object prefix. The later storage adapter must disable S3 Bucket Keys
for that profile so the encryption context remains an object ARN, then qualify
actual multipart backup writes and denied foreign-prefix access. Mock IAM source
checks alone do not qualify that provider behavior.

## INT-HOST-03: runtime-only PostgreSQL storage

- Owner: integrator; source work is held until v1.15 is independently reviewed and merged and the exclusive source claim is confirmed.
- Status: completed at the source and actual local TLS PostgreSQL boundary through PR26; independent review and fresh landed checks passed. Production role and composition acceptance remain separate.
- Frozen contract: [runtime-only PostgreSQL storage](../contracts/runtime-storage.md), ADR 026. Preserve development `storage.Open`, `*storage.DB`, and `storage.Migrate`; later production composition must receive only the distinct RuntimeDB runtime credential.
- Initial owned paths: `storage/runtime.go` and `storage/runtime_test.go` only. Do not alter existing DB/migration code, services, module files, job-pool integration, production apphost, generator, reconcile command, executable wiring, or execution-state acceptance records.
- Required behavior: one private runtime pool, no migration method or raw pool/config accessor, explicit copied PEM trust roots and bounded configuration, pinned pgx parser isolation, pre-network verified TLS/configuration, sanitized storage-generated errors, context-bounded startup ping and idempotent close. `TxRunner` exposes only `WithTx`; callback errors remain unchanged on successful/already-done rollback and otherwise join only the safe transaction sentinel; callers own public mapping. Begin/commit failures preserve caller context errors. Callback panics attempt rollback and rethrow unchanged even if rollback fails.
- Verification: poison tests for all 24 supported pgx environment variables; strict PEM and service/passfile/TLS-file-content non-discovery negatives; actual isolated TLS PostgreSQL with a synthetic least-privilege role for positive DML, hostname/root denial, rollback, and denied DDL/migration-ledger/role operations; compile-negative `RuntimeDB` to `Migrate`; deterministic operation-versus-close, panic, rollback-failure, panic-plus-rollback-failure, cancellation, post-close and idempotent-close tests; scoped format, tests, race, vet and lint; genuine negative/restored check; independent exact-head review; guarded rebase merge and landed test.
- Stage chain: `T-INT-HOST-03.1` through `.6`; production reconciliation depends on `.6`. No source task is accepted until its separate implementation, verification, review, merge and landed receipts exist. The added dependency does not establish production database role, TLS, deployment, or operational qualification.


### T2.8 private transaction adapter slice

- Coordinator adopts the source-grounded private adapter prerequisite; this does not enable operation invocation.
- Owned paths: new `app/operation/sqltx.go` and `app/operation/sqltx_test.go` only. Existing registry, shared contracts, module files, migrations and executable wiring remain untouched.
- Implement a private non-embedded adapter of one supplied `*sql.Tx` to the existing `operation.DBTX` interface. Reject nil transactions, preserve context/arguments/errors and return a genuinely nil Rows interface on query error. Expose no transaction lifecycle, pool, retry, invocation entry point or authority bypass.
- Required real PostgreSQL checks cover transaction visibility, rollback, cancellation, post-completion behavior and exact transaction ownership. Missing fixture prerequisites fail visibly. Unit tests and source review alone do not accept this slice.
- Independent source/fix review, affected checks, guarded merge and landed verification precede any component acceptance. Full executor authority, replay, audit/effects and transport gates remain unchanged.

The private SQL result-type adapter is not the complete callback wrapper: v1.13
transaction-control rejection and invalidation of retained DBTX/Row/Rows remain
mandatory before anything is supplied as InvocationContext.DB. No exported
construction or dispatch path is added by this prerequisite.

- Private adapter component outcome: independently reviewed PR28 landed with actual PostgreSQL normal/race and fresh landed checks. Full T2.8 and the callback guards above remain open; see [receipt](../evidence/operation-sql-adapter-20261007.md).

## Next bounded integration batch

Coordinator retains shared contracts, module/migration ordering, wiring and plan state.
Preflight assignments below produce concrete proposals only; semantic amendments
require independent review and merge before source dispatch. Existing accepted
work and full T2.8 status are preserved.

- Service constructor lane: inspect session/email/login/recovery/MFA/protection/mail material transaction consumers; propose minimal transaction-only constructor admission, typed-nil rejection and legacy compatibility matrix. No source edits.
- Job store lane: inspect jobs/sqlstore raw-pool operations; propose a transaction-only store boundary preserving leases, row locking, database time and worker compatibility. No source edits.
- Callback guard lane: define exact SQL transaction-control rejection and retained DBTX/Row/Rows lifetime behavior against frozen operation semantics, including concurrency and actual PostgreSQL acceptance cases. No source edits.
- Executor seam lane: audit current authority/session, replay/capacity, audit/effect and migration dependencies; identify the next compatible finite contract slice, without enabling dispatch. No source edits.
- Local verification lane: revalidate previously reviewed fixture custody, cached image and resource controls; prepare service windows and exact-owned cleanup. Launch only when a concrete source verification window is assigned.

### T2.8 private replay primitives

Coordinator adopts two new files only: `app/operation/replay_primitives.go` and
`replay_primitives_test.go`. Implement the frozen domain-separated request hash
and bounded cached-result integrity validation using existing Definition/codecs.
No exported API, persistence, authority, handler invocation or dispatch is added.
Validate stored bytes against the registered replay-safe required-idempotency
bound, finite result kind, SHA-256 and output codec; return defensive copies and
safe private errors. Future replay integration must check current authority and
stored descriptor/revision/schema identity before disclosure. The checksum is
integrity evidence, not authentication. Independent known-answer and mutation/
restoration tests, package normal/race/vet/lint, separate exact-head review and
landed checks gate this pure component. Actual SQL/replay/transport acceptance
remains required for the full task, which stays IN_PROGRESS.

### Contract adoption candidates v1.16–v1.18

The service, job-store and callback guard contracts and ADRs 027–029 are concrete
coordinator proposals. Independent exact-head review and merge precede source
assignment. Initial source ownership: session/session.go and new transaction/runtime
tests; protection/limits.go and its package tests; jobs/sqlstore/store.go,
txrunner.go and package tests; four new callback guard files named by its contract.
No lane owns migrations, modules, apphost or executable wiring. Coordinator will
record each actual dispatch and its exact frozen revision. Full product acceptance
and production-role/provider/budget/DNS decisions remain separate gates.

### Frozen first source dispatch

PR30 landed v1.16–v1.18 at de3461086262d2c68a7f7bb770c8858f92a30bab after
independent design review and Scanner-panic correction. Assigned authors now own
session/session.go plus its new transaction/runtime tests; jobs/sqlstore/store.go,
txrunner.go and assigned package tests; and the four new callback guard files.
The coordinator retains all shared contracts, modules, migrations and wiring.
INT-HOST-04 tracks session/jobs through six explicit delivery stages; private
callback and replay components remain nested under in-progress T2.8. No full
production composition or executor acceptance is inferred.

### Additional v1.16 protection leaf

A separate author owns identity/protection/limits.go, protection_test.go and new
transaction_test.go/runtime_integration_test.go only. Preserve legacy Config and
New, copied keys, admission/prune/error semantics; add the frozen transaction-only
constructor. Real runtime-role replica/admission/prune tests and independent review
are mandatory. This leaf does not expand the session/jobs INT-HOST-04 acceptance
scope or qualify complete authentication or production composition.

### Additional v1.16 protected-material leaf

A separate author owns delivery/email/materialstore/store.go and new transaction/
runtime integration tests only. Add the frozen transaction constructor while
preserving encryption, key copying/rotation, scope/purpose/expiry and caller-owned
write transactions. Actual RuntimeDB verification requires its own reviewed
precreated email-material schema and DML role; the session/jobs profiles cannot
substitute. No mail delivery, production secret or host qualification is claimed.

Material-store lane ownership amendment: its legacy `store_test.go` may retain the setup configuration and close `cfg.DB` instead of the private transaction capability. This preserves existing test lifecycle without adding pool authority to the service. Independent review covers this fourth file.

Material runtime test correction: coordinator owns only `delivery/email/materialstore/runtime_integration_test.go` to accept the standard eleven-field runtime fixture configuration while preserving strict unknown/trailing rejection and adding an offline format regression. A different reviewer must clear this correction and actual required-service evidence. The fixture format and service implementation remain unchanged.

Callback correction: a separate author owns only the four guard files for the diagnosed cancellation assertion; the delivered correction changes the test alone to prove backend termination and rollback effects. A different final reviewer owns actual-service repetition. No handler/executor wiring is delegated.

Next frozen v1.16 MFA leaf: assigned author owns only `identity/mfa/mfa.go`, new `transaction_test.go` and `runtime_integration_test.go`, preserving legacy API/configuration and all authentication SQL/behavior. A dedicated precreated runtime profile and independent review are required; no existing fixture profile qualifies MFA. No writer-order, challenge freshness or executor authority repair is delegated. Coordinator owns shared progress and profile admission.

Next frozen v1.16 email leaf: author owns only `identity/email/email.go`, new `transaction_test.go` and `runtime_integration_test.go`. Existing legacy API, SQL, purpose/origin/budget behavior and error mappings remain fixed. Runtime qualification requires identity, jobs and protected material in the same precreated database with explicit runtime DML grants; separate fixture databases cannot prove atomic composition. Missing prerequisites fail visibly; source and local doubles do not qualify the service or a provider.

### Login and recovery v1.16 source leaves

The delegated integrator assigns separate isolated authors to the remaining
login and recovery constructors, based on reviewed main
`4e7afabc28635be4e88e7f3e1cf0de6f4cf2e1f7`. Each owns only its package's
primary implementation file and new `transaction_test.go` and
`runtime_integration_test.go`. Existing Config/New shapes and operation SQL,
policy, defaults and error mappings remain unchanged. No producer freshness,
writer ordering, migration, module or executable changes are delegated.

Each leaf requires a dedicated precreated same-database runtime profile with
all actual identity, mail, job and applicable workspace/session dependencies.
Required checks cover successful operations, callback rollback, cancellation
and actual commit failure after callback writes, with no staged credentials or
acknowledgement escaping a failed transaction. Missing profiles fail visibly.
A different exact-head reviewer, guarded merge and fresh landed checks remain
mandatory. These assignments add no acceptance or provider qualification.

A separate private fixture author may prepare finite profiles and offline
controls; independent exact-byte review precedes any bounded local launch.
All model sessions, including reviews and successors, require admission by the
existing shared dispatcher. Queued assignments are not running workers.

The coordinator retains shared contracts and plan state. Paid-access task T5.9
remains dependent on full T2.8 and current authority; transaction-only billing
repositories already exist and do not require replacement constructors.

Recovery compatibility amendment: the coordinator owns the single legacy
`identity/recovery/recovery_test.go` assignment from `service.cfg.DB` to
`service.db`. A different reviewer must clear the complete four-file candidate.

### Adopted v1.19 private boundary leaf

PR41 landed the independently reviewed design at
`83023dc38b8a31714a2f5a656c1ceb9808d7bec0`. The assigned source author owns only
`app/operation/operation.go`, new `callback_invocation.go` and
`callback_invocation_test.go`; existing guard files and all shared files remain
outside the leaf. Complete result boxing preserves nil-interface output, and
invalidation/drain must finish before codecs. No exported executor or authority
is delegated. Different exact-head review, actual runtime-role tests, a genuine
wrong-order negative/restored check and fresh landed evidence remain required.

Boundary source ownership update: the queued author request was withdrawn before
admission during shared-dispatch infrastructure unavailability. The already-admitted
coordinator authored only the three assigned files in a separate worktree. A
different exact-head source reviewer is mandatory; design review is not source
review and no self-approval is permitted.

### Proposed runtime host binding prerequisite

The coordinator owns proposed v1.20/ADR031. After independent design review and
landing, an explicitly assigned author may own only new
`apphost/runtime_binding.go`, `apphost/runtime_binding_test.go` and
`apphost/runtime_binding_integration_test.go` for the bounded read-only binding
check. Source needs separately reviewed finite BOUND/EMPTY/MISSING local runtime
profiles, actual TLS checks, independent source/fix review and landed checks.
The source slice grants no schema creation, local-host modification or full
production constructor authority. Existing foreign task claims are preserved.

The separate current-authority audit reads session and writer paths and returns
an exact-source proposal to the coordinator. It owns no tracked source or shared
contract. The coordinator retains amendment adoption, plan state and wiring.

### Proposed current-session freshness prerequisite

The coordinator owns v1.21/ADR032 adoption after independent design review and
landing. Only then may an explicitly assigned author change FindActiveSession
in `identity/store/store.go`, ActiveSessionAssurance in
`identity/store/assurance.go`, and add
`identity/store/session_freshness_integration_test.go`. Preserve all other
methods, foreign task claims, middleware/producer semantics and module/migration
bytes. Actual demonstrated two-connection waits, old-boundary rejection,
assurance downgrade, negative/restored and different exact-head review are
required. Full writer compatibility/provenance/authority remains separately gated.
