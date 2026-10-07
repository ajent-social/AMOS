# Initial transaction-only composition stages

Scope: session and durable job-store constructors only; other services and production composition remain gated.

#### Wave 18

- [x] T-INT-HOST-04.1 Freeze initial transaction-only composition seams  Owner: Coordinator  Est: TBD  kind: agent stage: preflight  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.6]  acc: [Frozen v1.16/v1.17 contracts are independently reviewed and landed; session and jobs file ownership, compatibility and actual-service gates are recorded.]
  - Acceptance: Frozen v1.16/v1.17 contracts are independently reviewed and landed; session and jobs file ownership, compatibility and actual-service gates are recorded.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 19

- [x] T-INT-HOST-04.2 Implement transaction-only session and jobs constructors  Owner: Assigned component authors  Est: TBD  kind: agent stage: implement  blocked-by: [E-SDLC-TX.T-INT-HOST-04.1]  acc: [Only assigned session and job-store files implement additive APIs, retain legacy public shapes and avoid pool/migration/lifecycle authority.]
  - Acceptance: Only assigned session and job-store files implement additive APIs, retain legacy public shapes and avoid pool/migration/lifecycle authority.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 20

- [x] T-INT-HOST-04.2.F1 Correct deterministic job lease waiter observation  Owner: Assigned jobs correction author  Est: TBD  kind: agent stage: implement  blocked-by: [E-SDLC-TX.T-INT-HOST-04.2]  acc: [The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.]
  - Acceptance: The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 20

- [x] T-INT-HOST-04.3 Verify actual session and job-store runtime composition  Owner: Assigned component authors  Est: TBD  kind: agent stage: verify  blocked-by: [E-SDLC-TX.T-INT-HOST-04.2]  acc: [Actual precreated TLS PostgreSQL runtime-role tests prove session operations and job atomicity, cancellation and post-lock fencing; unit/race/vet/lint and negative/restored checks pass without skipped prerequisites.]
  - Acceptance: Actual precreated TLS PostgreSQL runtime-role tests prove session operations and job atomicity, cancellation and post-lock fencing; unit/race/vet/lint and negative/restored checks pass without skipped prerequisites.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 21

- [x] T-INT-HOST-04.3.F1 Verify corrected job lease regression against old behavior  Owner: Assigned jobs correction author  Est: TBD  kind: agent stage: verify  blocked-by: [E-SDLC-TX.T-INT-HOST-04.2.F1]  acc: [The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.]
  - Acceptance: The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 22

- [ ] T-INT-HOST-04.4 Independently review initial transaction-only components  Owner: Independent Astra low reviewer  Est: TBD  kind: agent stage: review  blocked-by: [E-SDLC-TX.T-INT-HOST-04.3, E-SDLC-TX.T-INT-HOST-04.3.F1]  acc: [Different exact-head reviewers clear both complete source slices and actual service evidence; accepted findings receive correction and distinct review before merge.]
  - Acceptance: Different exact-head reviewers clear both complete source slices and actual service evidence; accepted findings receive correction and distinct review before merge.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 23

- [ ] T-INT-HOST-04.5 Merge initial transaction-only components  Owner: Coordinator  Est: TBD  kind: agent stage: merge  blocked-by: [E-SDLC-TX.T-INT-HOST-04.4]  acc: [Both reviewed source slices land through guarded PR rebase merges after immediate head/base/check readback, preserving protected policy.]
  - Acceptance: Both reviewed source slices land through guarded PR rebase merges after immediate head/base/check readback, preserving protected policy.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).

#### Wave 24

- [ ] T-INT-HOST-04.6 Verify landed session and job-store components  Owner: Coordinator  Est: TBD  kind: agent stage: verify-landed  blocked-by: [E-SDLC-TX.T-INT-HOST-04.5]  acc: [Both landed source slices match reviewed bytes and fresh actual-service checks pass. Remaining service constructors, production host and provider/production-role qualification remain open.]
  - Acceptance: Both landed source slices match reviewed bytes and fresh actual-service checks pass. Remaining service constructors, production host and provider/production-role qualification remain open.
  - Contracts: [services](../../contracts/transaction-services.md), [job store](../../contracts/transaction-job-store.md).
