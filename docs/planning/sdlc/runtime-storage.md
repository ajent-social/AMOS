# Runtime-only PostgreSQL delivery stages

Acceptance: Independently reviewed runtime-only storage source is merged and locally verified; production grants and deployment remain separate.

Status: planned; no source implementation, execution or acceptance is asserted.

#### Wave 10

- [ ] T-INT-HOST-03.1 Reconcile runtime-only PostgreSQL assignment  Owner: Coordinator  Est: TBD  kind: agent stage: preflight  blocked-by: [E-SDLC-PROD.T-PROD.15, E-SDLC-HOST.T-INT-HOST-02.6]  acc: [Frozen v1.15 design is reviewed and landed; exact two-file ownership, exclusive claim, isolated TLS fixture capacity and legacy API compatibility are recorded before dispatch.]
  - Acceptance: Frozen v1.15 design is reviewed and landed; exact two-file ownership, exclusive claim, isolated TLS fixture capacity and legacy API compatibility are recorded before dispatch.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).

#### Wave 11

- [ ] T-INT-HOST-03.2 Implement runtime-only PostgreSQL pool  Owner: Storage worker  Est: TBD  kind: agent stage: implement  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.1]  acc: [Only storage/runtime.go and storage/runtime_test.go implement the distinct one-pool RuntimeDB and TxRunner boundary; explicit verified TLS and ambient configuration rejection preserve legacy Open, DB and Migrate APIs.]
  - Acceptance: Only storage/runtime.go and storage/runtime_test.go implement the distinct one-pool RuntimeDB and TxRunner boundary; explicit verified TLS and ambient configuration rejection preserve legacy Open, DB and Migrate APIs.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).

#### Wave 12

- [ ] T-INT-HOST-03.3 Verify runtime-only PostgreSQL isolation  Owner: Storage worker  Est: TBD  kind: agent stage: verify  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.2]  acc: [Required real TLS PostgreSQL checks prove DML, rollback, denied DDL and migration-role authority, root and hostname failures, bounded startup, safe failures and compile-negative Migrate incompatibility; scoped race/vet/lint and genuine negative/restored evidence pass.]
  - Acceptance: Required real TLS PostgreSQL checks prove DML, rollback, denied DDL and migration-role authority, root and hostname failures, bounded startup, safe failures and compile-negative Migrate incompatibility; scoped race/vet/lint and genuine negative/restored evidence pass.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).

#### Wave 13

- [ ] T-INT-HOST-03.4 Independently review runtime-only PostgreSQL  Owner: Independent GPT-6-Luna reviewer  Est: TBD  kind: agent stage: review  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.3]  acc: [A separate reviewer clears exact source head and base, isolation and compatibility; accepted findings receive explicit fix, affected verification and re-review stages before merge.]
  - Acceptance: A separate reviewer clears exact source head and base, isolation and compatibility; accepted findings receive explicit fix, affected verification and re-review stages before merge.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).

#### Wave 14

- [ ] T-INT-HOST-03.5 Rebase merge runtime-only PostgreSQL  Owner: Coordinator  Est: TBD  kind: agent stage: merge  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.4]  acc: [The independently reviewed exact head lands through guarded GitHub rebase merge without bypassing branch policy; local verification is distinguished from hosted CI.]
  - Acceptance: The independently reviewed exact head lands through guarded GitHub rebase merge without bypassing branch policy; local verification is distinguished from hosted CI.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).

#### Wave 15

- [ ] T-INT-HOST-03.6 Verify landed runtime-only PostgreSQL  Owner: Coordinator  Est: TBD  kind: agent stage: verify-landed  blocked-by: [E-SDLC-RUNTIME.T-INT-HOST-03.5]  acc: [Landed files equal reviewed bytes, affected checks pass and bounded source receipts are recorded; consumer migration, production grants, migration-secret absence and deployment remain open.]
  - Acceptance: Landed files equal reviewed bytes, affected checks pass and bounded source receipts are recorded; consumer migration, production grants, migration-secret absence and deployment remain open.
  - Contract: [runtime-only storage](../../contracts/runtime-storage.md); ownership: [integration assignment](../integration-assignments.md).
