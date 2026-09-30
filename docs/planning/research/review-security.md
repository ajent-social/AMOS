# Independent planning review: security and architecture

Status: read-only review of the current planning inputs. **Three material corrections are recommended before dispatch.** These are gaps or ambiguities in future task contracts, not claims of vulnerabilities in implemented code. No source was implemented, tests built, external action launched or spending incurred.

Reviewed scope: [VISION](../../VISION.md), [RFC 0001](../../rfc/rfc-0001.md), [cross-lane contracts](../contracts.md), the bounded-maintenance ADR, and all four JSON inputs in `docs/planning/inputs/`. Changes were limited to this review file. The root is concurrently integrating the inventory, so task IDs are the durable references.

## R1  -  Bind automatic promotion to the installed release, not only the candidate

**Priority: P1. Affects T13.1, T13.9, T13.10 and T9.14.**

The plan binds evidence to candidate source/artifact/policy and handles duplicate requests and lost deployment acknowledgments. T9.14 also serializes deployments. It does not explicitly bind an eligible automatic decision to the **expected currently installed release and target generation**, or require a fenced compare-and-swap at the final deploy boundary. Serialization alone does not reject a stale queued deployment.

Failure case: jobs A and B are independently verified against release R. B deploys first. A later acquires the serialized deployment slot and presents a still-valid signed decision. Deploying A can overwrite B, roll back a security fix, or act against a schema state different from the one qualified for A. Neither duplicate-request protection nor an exact candidate digest detects this: A is a different legitimate request with a legitimate artifact.

Minimal correction:

- Add expected base release, expected schema compatibility state, target identity/generation and decision expiry to the maintenance/release contract.
- Require final release execution to atomically validate or fence the current installed generation, latest effective policy revision and pause generation before obtaining/using deploy authority. An intervening release or policy change invalidates eligibility and requires requalification.
- Keep same-request unknown-outcome reconciliation idempotent; it must not allocate a new generation merely to bypass the stale-decision failure.
- Add one negative test: qualify A and B against R, deploy B, then attempt A. A must be held for rebase/reverification and must not overwrite B. Repeat with an owner policy/pause change between eligibility and execution.

This extends the existing immutable-promotion and replay contracts; it does not require a new orchestration service.

## R2  -  Make restore cutover depend on a verified non-restored authority barrier

**Priority: P1. Affects T10.11, T3.23, T15.13 and maintenance restore behavior.**

T3.23 proposes a monotonic security epoch and tests an old backup while a newer barrier remains authoritative. T15.13 tests credential non-revival. However, T10.11's executable cutover task currently depends on isolated restore plus billing/outbox reconciliation, and merely lists session/signing reconciliation and credential rotation needs. It has no explicit T3.23 prerequisite or mandatory authority-barrier check before traffic switch. The plan also does not say what happens when the newer barrier is unavailable or was itself restored backward.

Failure case: restore a snapshot predating a session/API-key/OAuth revocation, or predating an owner maintenance pause. If the authority epoch, pause generation and old eligible decisions are restored together, a syntactically valid restored record can become usable again. A backup test that assumes an intact newer barrier does not cover this failure.

Minimal correction:

- Make restore activation/cutover consume the implemented security-epoch contract and prove the authoritative epoch cannot silently roll back with the application backup. A fresh externally established activation epoch that invalidates old authority, or another explicitly qualified monotonic design, can satisfy this; do not leave the storage/loss behavior implicit.
- Keep restored service isolated and credentialed effects disabled when barrier freshness cannot be proven. Operator confirmation alone must not turn unknown freshness into acceptance.
- Cover sessions, pending proof challenges, API keys and OAuth grants; invalidate or requalify restored maintenance leases, eligibility decisions, pause/resume generations and release credentials before maintenance resumes.
- Wire T10.11 after the epoch implementation. If T3.23 consumes a restore harness, map that prerequisite to **T10.10's isolated harness**, not T10.11 cutover, to avoid creating a cycle.
- Add tests for a restored pre-revocation snapshot with missing/stale barrier, and a restored pre-pause snapshot containing an old signed promotion decision. Both remain held; neither authenticates nor promotes until explicit safe activation/requalification.

T15.13 can remain the composed qualification task. The enforcement must be an implementation requirement of the actual activation boundary, not solely a later test description.

## R3  -  Narrow the extension tasks to consumers of the new shared authorities

**Priority: P1 dispatch ambiguity. Affects T2.8/T12.3 and T2.9/T12.6.**

The new root-owned T2.8 correctly establishes `app/operation` and `app/policy` as the shared invocation authority. T12.3 still says to provide the ordinary domain invocation boundary, apply authorization/entitlement/retry checks, and owns a separate `internal/businessinvoke` implementation. Similarly, T2.9 owns signed private-service identity construction/verification, while T12.6 still instructs its lane to create and verify the context in a separate package. The unresolved prerequisites can point to the root tasks, but the instructions still let a worker build competing policy or wire-format logic.

Minimal correction:

- Resolve T12.3's evaluator/transaction prerequisite to T2.8 and rewrite its scope as business registration and transport adaptation **delegating to** `app/operation`. It must not implement a second authorization, entitlement, idempotency or transaction policy.
- Resolve T12.6's signer prerequisite to T2.9 and describe it as context mapping and separate-service conformance **using** the canonical signer/verifier. Declare one owner for assertion encoding, trust/key rotation and replay rules.
- Add a small acceptance assertion that integrated/proxy adapters invoke the canonical service once and cannot proceed after it denies; retain the existing real-boundary tests.

This is a task-contract clarification and dependency resolution, not additional product scope.

## Checks that found no material defect

The latest review snapshot contained **255 tasks**. Explicit task-ID dependency traversal found no missing IDs, cycles or stage inversions. There were still **66 unresolved `REQUIRES:` edges covering 64 distinct descriptions**. The complete graph must be checked again after those are resolved, particularly the restore implementation/qualification split above. This review does not certify the eventual resolved graph.

The product boundary remains consistent: external MCP clients receive application authorization, entitlement and resumable proof; no customer-agent governance inbox or hosted agent runtime is added. Separate owner/upstream maintenance identities, protected checks, bounded cost/egress, private vulnerability handling and security-sensitive AMSL human review are already represented. Diagnostics explicitly exclude arbitrary private source frames/raw messages and require synthetic reproducibility. No expansion of those scopes is recommended.

A targeted public-data scan of the reviewed documentation found no actual home path, private consumer name, account identifier or attendee/scheduling disclosure. Its only candidate was generic wording about clearing restricted-source imports, which is not private content. This is a targeted review, not a guarantee that every future artifact is publication-safe.

No current standards, provider behavior or implementation conformance was inferred from the plan. Its primary-source protocol/toolchain freeze tasks and E16 live evidence gates remain necessary. Keep local implementation, merged integration, released artifact and observed live behavior as separate completion states.
