# Delivery inventory 01

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
Registry: `docs/planning/execution-state.json`; `sha256:9d71523c552bbd2b27f347165f79059304cc51a92b2b8c52b292a750eb046530`.
Registry: `docs/planning/sdlc-stage-state.json`; `sha256:6ae4b0594209343bcb0f27305da70a96d562807833073626eba0d21932269004`.
See [projection contract](../planning/portable-plan-export.md) for regeneration and limits.
<!-- current-native-projection:end -->

- [ ] T-FEATURE-BINDING.1 [Implement reviewed feature view operation binding: preflight]
  Stage: preflight
  canonical-id: T-FEATURE-BINDING.1
  authored-stage: preflight
  deps: [T-FEATURE-CONTRACT.6, T12.1, T2.8]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-FEATURE-BINDING.2 [Implement reviewed feature view operation binding: author]
  Stage: implement
  canonical-id: T-FEATURE-BINDING.2
  authored-stage: author
  deps: [T-FEATURE-BINDING.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-FEATURE-BINDING.3 [Implement reviewed feature view operation binding: verify]
  Stage: verify
  canonical-id: T-FEATURE-BINDING.3
  authored-stage: verify
  deps: [T-FEATURE-BINDING.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-FEATURE-BINDING.4 [Implement reviewed feature view operation binding: review]
  Stage: review
  canonical-id: T-FEATURE-BINDING.4
  authored-stage: review
  deps: [T-FEATURE-BINDING.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [ ] T-FEATURE-BINDING.5 [Implement reviewed feature view operation binding: merge]
  Stage: merge
  canonical-id: T-FEATURE-BINDING.5
  authored-stage: merge
  deps: [T-FEATURE-BINDING.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [ ] T-FEATURE-BINDING.6 [Implement reviewed feature view operation binding: landed]
  Stage: verify-landed
  canonical-id: T-FEATURE-BINDING.6
  authored-stage: landed
  deps: [T-FEATURE-BINDING.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Deterministic domain-neutral generation and view binding preserve operation policy and authored overrides; missing schema/components/authority fail closed. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [ ] T-FEATURE-CONTRACT.1 [Adopt generic feature view operation definitions: preflight]
  Stage: preflight
  canonical-id: T-FEATURE-CONTRACT.1
  authored-stage: preflight
  deps: [T-RPL-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-FEATURE-CONTRACT.2 [Adopt generic feature view operation definitions: author]
  Stage: implement
  canonical-id: T-FEATURE-CONTRACT.2
  authored-stage: author
  deps: [T-FEATURE-CONTRACT.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-FEATURE-CONTRACT.3 [Adopt generic feature view operation definitions: verify]
  Stage: verify
  canonical-id: T-FEATURE-CONTRACT.3
  authored-stage: verify
  deps: [T-FEATURE-CONTRACT.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-FEATURE-CONTRACT.4 [Adopt generic feature view operation definitions: review]
  Stage: review
  canonical-id: T-FEATURE-CONTRACT.4
  authored-stage: review
  deps: [T-FEATURE-CONTRACT.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [ ] T-FEATURE-CONTRACT.5 [Adopt generic feature view operation definitions: merge]
  Stage: merge
  canonical-id: T-FEATURE-CONTRACT.5
  authored-stage: merge
  deps: [T-FEATURE-CONTRACT.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [ ] T-FEATURE-CONTRACT.6 [Adopt generic feature view operation definitions: landed]
  Stage: verify-landed
  canonical-id: T-FEATURE-CONTRACT.6
  authored-stage: landed
  deps: [T-FEATURE-CONTRACT.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Resolve RFC0002 vocabulary, authoritative OpenAPI/policy references, stable identities, strict rejection and app/framework ownership; no parallel permission model. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [ ] T-FEATURE-EVOLUTION.1 [Implement reviewed feature evolution lifecycle: preflight]
  Stage: preflight
  canonical-id: T-FEATURE-EVOLUTION.1
  authored-stage: preflight
  deps: [T-FEATURE-BINDING.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-FEATURE-EVOLUTION.2 [Implement reviewed feature evolution lifecycle: author]
  Stage: implement
  canonical-id: T-FEATURE-EVOLUTION.2
  authored-stage: author
  deps: [T-FEATURE-EVOLUTION.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-FEATURE-EVOLUTION.3 [Implement reviewed feature evolution lifecycle: verify]
  Stage: verify
  canonical-id: T-FEATURE-EVOLUTION.3
  authored-stage: verify
  deps: [T-FEATURE-EVOLUTION.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-FEATURE-EVOLUTION.4 [Implement reviewed feature evolution lifecycle: review]
  Stage: review
  canonical-id: T-FEATURE-EVOLUTION.4
  authored-stage: review
  deps: [T-FEATURE-EVOLUTION.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [ ] T-FEATURE-EVOLUTION.5 [Implement reviewed feature evolution lifecycle: merge]
  Stage: merge
  canonical-id: T-FEATURE-EVOLUTION.5
  authored-stage: merge
  deps: [T-FEATURE-EVOLUTION.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [ ] T-FEATURE-EVOLUTION.6 [Implement reviewed feature evolution lifecycle: landed]
  Stage: verify-landed
  canonical-id: T-FEATURE-EVOLUTION.6
  authored-stage: landed
  deps: [T-FEATURE-EVOLUTION.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed definition diff handles rename/removal, queued work, append-only migration, compatibility, preimage conflicts and forward repair without automatic data destruction. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-INT-HOST-02.1 [Reconcile finite host routing assignment]
  Stage: preflight
  canonical-id: T-INT-HOST-02.1
  authored-stage: preflight
  deps: [T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Frozen v1.14 design, exact ownership and exclusive claim are recorded; legacy route and telemetry behavior is reconciled without production claims.]

- [x] T-INT-HOST-02.2 [Implement finite host routing]
  Stage: implement
  canonical-id: T-INT-HOST-02.2
  authored-stage: implement
  deps: [T-INT-HOST-02.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Protocol slots, business manifest, immutable routing snapshot and legacy compatibility follow the frozen contract in the assigned paths.]

- [x] T-INT-HOST-02.3 [Verify finite host routing]
  Stage: verify
  canonical-id: T-INT-HOST-02.3
  authored-stage: verify
  deps: [T-INT-HOST-02.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Required route/method/auth-equivalence, alias/reserved and deterministic freeze tests pass; actual local HTTP checks, race/vet/lint and genuine negative/restored evidence are recorded.]

- [x] T-INT-HOST-02.4 [Independently review finite host routing]
  Stage: review
  canonical-id: T-INT-HOST-02.4
  authored-stage: review
  deps: [T-INT-HOST-02.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [A separate reviewer clears the exact source head and base; accepted findings require explicit fix, affected verification and re-review stages before merge.]

- [x] T-INT-HOST-02.5 [Rebase merge finite host routing]
  Stage: merge
  canonical-id: T-INT-HOST-02.5
  authored-stage: merge
  deps: [T-INT-HOST-02.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [The reviewed exact head lands through guarded GitHub rebase merge without bypassing branch policy; local and hosted evidence remain distinct.]

- [x] T-INT-HOST-02.6 [Verify landed finite host routing]
  Stage: verify-landed
  canonical-id: T-INT-HOST-02.6
  authored-stage: verify-landed
  deps: [T-INT-HOST-02.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Landed source matches reviewed files, affected checks pass and execution receipts record bounded routing acceptance; protocol/provider/production gates remain open.]

- [x] T-INT-HOST-03.1 [Reconcile runtime-only PostgreSQL assignment]
  Stage: preflight
  canonical-id: T-INT-HOST-03.1
  authored-stage: preflight
  deps: [T-PROD.15, T-INT-HOST-02.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Frozen v1.15 design is reviewed and landed; exact two-file ownership, exclusive claim, isolated TLS fixture capacity and legacy API compatibility are recorded before dispatch.]

- [x] T-INT-HOST-03.2 [Implement runtime-only PostgreSQL pool]
  Stage: implement
  canonical-id: T-INT-HOST-03.2
  authored-stage: implement
  deps: [T-INT-HOST-03.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Only storage/runtime.go and storage/runtime_test.go implement the distinct one-pool RuntimeDB and TxRunner boundary; explicit verified TLS and ambient configuration rejection preserve legacy Open, DB and Migrate APIs.]

- [x] T-INT-HOST-03.2.F1 [Fix runtime required-service coverage and strict PEM rejection]
  Stage: implement
  canonical-id: T-INT-HOST-03.2.F1
  authored-stage: implement
  deps: [T-INT-HOST-03.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.2.F2 [Fix actual-service cancellation assertions]
  Stage: implement
  canonical-id: T-INT-HOST-03.2.F2
  authored-stage: implement
  deps: [T-INT-HOST-03.2.F1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.3 [Verify runtime-only PostgreSQL isolation]
  Stage: verify
  canonical-id: T-INT-HOST-03.3
  authored-stage: verify
  deps: [T-INT-HOST-03.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Required real TLS PostgreSQL checks prove DML, rollback, denied DDL and migration-role authority, root and hostname failures, bounded startup, safe failures and compile-negative Migrate incompatibility; scoped race/vet/lint and genuine negative/restored evidence pass.]

- [x] T-INT-HOST-03.3.F1 [Verify runtime review corrections and negative evidence]
  Stage: verify
  canonical-id: T-INT-HOST-03.3.F1
  authored-stage: verify
  deps: [T-INT-HOST-03.2.F1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.3.F2 [Verify actual-service cancellation correction]
  Stage: verify
  canonical-id: T-INT-HOST-03.3.F2
  authored-stage: verify
  deps: [T-INT-HOST-03.2.F2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.4 [Independently review runtime-only PostgreSQL]
  Stage: review
  canonical-id: T-INT-HOST-03.4
  authored-stage: review
  deps: [T-INT-HOST-03.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [A separate reviewer clears exact source head and base, isolation and compatibility; accepted findings receive explicit fix, affected verification and re-review stages before merge.]

- [x] T-INT-HOST-03.4.R1 [Independently review final runtime corrections]
  Stage: review
  canonical-id: T-INT-HOST-03.4.R1
  authored-stage: review
  deps: [T-INT-HOST-03.3.F1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.4.R2 [Independently review actual-service final source]
  Stage: review
  canonical-id: T-INT-HOST-03.4.R2
  authored-stage: review
  deps: [T-INT-HOST-03.3.F2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact source correction, actual affected verification and distinct-author review are required before merge; no provider or production qualification.]

- [x] T-INT-HOST-03.5 [Rebase merge runtime-only PostgreSQL]
  Stage: merge
  canonical-id: T-INT-HOST-03.5
  authored-stage: merge
  deps: [T-INT-HOST-03.4.R2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [The independently reviewed exact head lands through guarded GitHub rebase merge without bypassing branch policy; local verification is distinguished from hosted CI.]

- [x] T-INT-HOST-03.6 [Verify landed runtime-only PostgreSQL]
  Stage: verify-landed
  canonical-id: T-INT-HOST-03.6
  authored-stage: verify-landed
  deps: [T-INT-HOST-03.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Landed files equal reviewed bytes, affected checks pass and bounded source receipts are recorded; consumer migration, production grants, migration-secret absence and deployment remain open.]

- [x] T-INT-HOST-04.1 [Freeze initial transaction-only composition seams]
  Stage: preflight
  canonical-id: T-INT-HOST-04.1
  authored-stage: preflight
  deps: [T-INT-HOST-03.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Frozen v1.16/v1.17 contracts are independently reviewed and landed; session and jobs file ownership, compatibility and actual-service gates are recorded.]

- [x] T-INT-HOST-04.2 [Implement transaction-only session and jobs constructors]
  Stage: implement
  canonical-id: T-INT-HOST-04.2
  authored-stage: implement
  deps: [T-INT-HOST-04.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Only assigned session and job-store files implement additive APIs, retain legacy public shapes and avoid pool/migration/lifecycle authority.]

- [x] T-INT-HOST-04.2.F1 [Correct deterministic job lease waiter observation]
  Stage: implement
  canonical-id: T-INT-HOST-04.2.F1
  authored-stage: implement
  deps: [T-INT-HOST-04.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.]

- [x] T-INT-HOST-04.3 [Verify actual session and job-store runtime composition]
  Stage: verify
  canonical-id: T-INT-HOST-04.3
  authored-stage: verify
  deps: [T-INT-HOST-04.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Actual precreated TLS PostgreSQL runtime-role tests prove session operations and job atomicity, cancellation and post-lock fencing; unit/race/vet/lint and negative/restored checks pass without skipped prerequisites.]

- [x] T-INT-HOST-04.3.F1 [Verify corrected job lease regression against old behavior]
  Stage: verify
  canonical-id: T-INT-HOST-04.3.F1
  authored-stage: verify
  deps: [T-INT-HOST-04.2.F1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [The author delivers the test-only correction and demonstrates the intended stale-success negative with exact restoration and actual normal/race checks; distinct final review remains the separate review-stage gate.]

- [x] T-INT-HOST-04.4 [Independently review initial transaction-only components]
  Stage: review
  canonical-id: T-INT-HOST-04.4
  authored-stage: review
  deps: [T-INT-HOST-04.3, T-INT-HOST-04.3.F1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Different exact-head reviewers clear both complete source slices and actual service evidence; accepted findings receive correction and distinct review before merge.]

- [x] T-INT-HOST-04.5 [Merge initial transaction-only components]
  Stage: merge
  canonical-id: T-INT-HOST-04.5
  authored-stage: merge
  deps: [T-INT-HOST-04.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Both reviewed source slices land through guarded PR rebase merges after immediate head/base/check readback, preserving protected policy.]

- [x] T-INT-HOST-04.6 [Verify landed session and job-store components]
  Stage: verify-landed
  canonical-id: T-INT-HOST-04.6
  authored-stage: verify-landed
  deps: [T-INT-HOST-04.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Both landed source slices match reviewed bytes and fresh actual-service checks pass. Remaining service constructors, production host and provider/production-role qualification remain open.]

- [ ] T-OPTIONAL-CONTEXT.1 [Adopt optional host context and bridge retirement: preflight]
  Stage: preflight
  canonical-id: T-OPTIONAL-CONTEXT.1
  authored-stage: preflight
  deps: [T-RPL-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-OPTIONAL-CONTEXT.2 [Adopt optional host context and bridge retirement: author]
  Stage: implement
  canonical-id: T-OPTIONAL-CONTEXT.2
  authored-stage: author
  deps: [T-OPTIONAL-CONTEXT.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-OPTIONAL-CONTEXT.3 [Adopt optional host context and bridge retirement: verify]
  Stage: verify
  canonical-id: T-OPTIONAL-CONTEXT.3
  authored-stage: verify
  deps: [T-OPTIONAL-CONTEXT.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-OPTIONAL-CONTEXT.4 [Adopt optional host context and bridge retirement: review]
  Stage: review
  canonical-id: T-OPTIONAL-CONTEXT.4
  authored-stage: review
  deps: [T-OPTIONAL-CONTEXT.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [ ] T-OPTIONAL-CONTEXT.5 [Adopt optional host context and bridge retirement: merge]
  Stage: merge
  canonical-id: T-OPTIONAL-CONTEXT.5
  authored-stage: merge
  deps: [T-OPTIONAL-CONTEXT.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [ ] T-OPTIONAL-CONTEXT.6 [Adopt optional host context and bridge retirement: landed]
  Stage: verify-landed
  canonical-id: T-OPTIONAL-CONTEXT.6
  authored-stage: landed
  deps: [T-OPTIONAL-CONTEXT.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent standalone operation, exact scoped disclosure, separately owned development/review/release, installation-specific bridge retirement, at most one effect dispatcher and reconciliation of unknown outcomes are explicit reviewed contracts; no private endpoints. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-PROD.1 [Reconcile source, task authority, ownership and runtime capacity]
  Stage: preflight
  canonical-id: T-PROD.1
  authored-stage: preflight
  deps: []
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Fetched main, clean or preserved changes, current execution registry, task and integrator claims, frozen contract revision and active process working directories are reconciled; SSD and build lease are usable; independent reviewers and available GPT-6-Luna capacity are recorded; ordinary repository lifecycle is confirmed or an enrolled authoritative binding replaces affected chains.]

- [ ] T-PROD.10 [Run production AWS deployment at https://amos.sire.run]
  Stage: implement
  canonical-id: T-PROD.10
  authored-stage: implement
  deps: [T-PROD.9]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Scoped deployment applies the reviewed infrastructure/artifact, performs serialized approved migrations, activates required providers and publishes the HTTPS domain without exposing database/internal services; deployed source/digest and sanitized rollout receipt are recorded; failed rollout follows the reviewed recovery procedure.]

- [ ] T-PROD.11 [Verify full production behavior at https://amos.sire.run]
  Stage: verify
  canonical-id: T-PROD.11
  authored-stage: verify
  deps: [T-PROD.10]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent external browser and HTTP checks confirm DNS/TLS, deployed digest, health/readiness, signup/verification/sign-in, workspace/business CRUD, organization/role denials, approved real billing lifecycle and entitlements, REST/MCP authorization, durable restart and background jobs; no test data/customer mutations or real charges exceed approved scope; required provider checks pass in the production-configured mode or full completion remains blocked.]

- [ ] T-PROD.12 [Verify operating stability, alerts and recovery readiness]
  Stage: verify
  canonical-id: T-PROD.12
  authored-stage: verify
  deps: [T-PROD.11]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Owner-selected observation window, SLO/error/load/cost targets pass; jobs and provider reconciliation remain healthy, backups complete with a demonstrated isolated restore, alerts deliver to authorized operator endpoints, rollback artifact remains usable and runbooks name suspension/rotation/recovery responsibilities; missing targets or evidence block completion.]

- [ ] T-PROD.13 [Independently accept complete running production and handoff]
  Stage: review
  canonical-id: T-PROD.13
  authored-stage: review
  deps: [T-PROD.12]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer binds complete product evidence, release/source/digest, production URL, selected profile, live and operations receipts to a final handoff; coordinator records terminal completion only with every reachable required gate satisfied and no blocked required capability. Local acceptance, release publication and an HTTP 200 alone cannot satisfy this item.]

- [x] T-PROD.14 [Accept existing product requirements and constraints]
  Stage: preflight
  canonical-id: T-PROD.14
  authored-stage: preflight
  deps: [T-PROD.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Existing approved product scope, original task contracts, mapped use cases and currently documented privacy and regulatory constraints are reconciled against docs/VISION.md, docs/planning/requirements.md and docs/planning/contracts.md. No product capability, use case or regulatory obligation is added, removed or narrowed. Any contradiction or unresolved interpretation is recorded as an owner decision and blocks affected downstream work; all 257 original task IDs and accepted evidence remain unchanged. Completion additionally requires independent domain/content review by a reviewer distinct from the preparer, with the reviewed scope and baseline revision, validation evidence, stable findings and their dispositions recorded. Unresolved review findings block completion and every dependent gate.]

- [x] T-PROD.15 [Qualify crosscutting design contracts against accepted scope]
  Stage: verify
  canonical-id: T-PROD.15
  authored-stage: verify
  deps: [T-PROD.14]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Crosscutting design assumptions in docs/VISION.md, docs/rfc/rfc-0001.md, accepted ADRs and docs/planning/contracts.md are qualified against the accepted requirements baseline, with authority, privacy, tenancy, interface ownership, billing, migrations, provider failure and recovery boundaries consistent. Conflicts are recorded as owner decisions and block affected work. Existing subsystem task owners retain their specific designs; this gate creates no competing design or new product scope. Completion additionally requires independent domain/content review by a reviewer distinct from the preparer, with the reviewed scope and baseline revision, validation evidence, stable findings and their dispositions recorded. Unresolved review findings block completion and every dependent gate.]

- [ ] T-PROD.2 [Resolve AWS production and required-provider prerequisites]
  Stage: preflight
  canonical-id: T-PROD.2
  authored-stage: preflight
  deps: [T-PROD.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Owner AWS account and region, selected production profile, budget ceiling and spend alarms, domain/DNS control, approved provider modes, secret references, release authority and rollback limits are recorded privately with public-safe status; missing prerequisites are explicit BLOCKED gates, never inferred from installed CLIs.]

- [ ] T-PROD.3 [Reconcile complete product capability and production coverage]
  Stage: verify
  canonical-id: T-PROD.3
  authored-stage: verify
  deps: [T-PROD.2, T-SDLC-1-13.6, T-SDLC-2-5.6, T-SDLC-2-7.6, T-SDLC-2-8.6, T-SDLC-2-9.6, T-SDLC-3-11.6, T-SDLC-3-12.6, T-SDLC-3-13.6, T-SDLC-3-14.6, T-SDLC-3-16.6, T-SDLC-3-17.6, T-SDLC-3-18.6, T-SDLC-3-19.6, T-SDLC-3-20.6, T-SDLC-3-21.6, T-SDLC-3-22.6, T-SDLC-3-23.6, T-SDLC-4-5.6, T-SDLC-4-6.6, T-SDLC-4-7.6, T-SDLC-4-8.6, T-SDLC-4-9.6, T-SDLC-4-10.6, T-SDLC-4-11.6, T-SDLC-4-12.6, T-SDLC-4-13.6, T-SDLC-4-14.6, T-SDLC-5-9.6, T-SDLC-5-10.6, T-SDLC-5-11.6, T-SDLC-5-12.6, T-SDLC-5-13.6, T-SDLC-5-14.6, T-SDLC-5-15.6, T-SDLC-5-16.6, T-SDLC-5-17.6, T-SDLC-5-18.6, T-SDLC-5-19.6, T-SDLC-5-20.6, T-SDLC-5-21.6, T-SDLC-5-22.6, T-SDLC-6-5.6, T-SDLC-6-6.6, T-SDLC-6-7.6, T-SDLC-6-8.6, T-SDLC-6-9.6, T-SDLC-6-10.6, T-SDLC-6-11.6, T-SDLC-6-12.6, T-SDLC-6-13.6, T-SDLC-6-15.6, T-SDLC-6-16.6, T-SDLC-6-17.6, T-SDLC-6-18.6, T-SDLC-7-6.6, T-SDLC-7-8.6, T-SDLC-7-9.6, T-SDLC-7-10.6, T-SDLC-7-11.6, T-SDLC-7-12.6, T-SDLC-7-13.6, T-SDLC-7-14.6, T-SDLC-7-15.6, T-SDLC-7-16.6, T-SDLC-8-3.6, T-SDLC-8-4.6, T-SDLC-8-5.6, T-SDLC-8-6.6, T-SDLC-8-7.6, T-SDLC-8-8.6, T-SDLC-8-9.6, T-SDLC-8-10.6, T-SDLC-8-11.6, T-SDLC-8-12.6, T-SDLC-8-13.6, T-SDLC-8-14.6, T-SDLC-8-15.6, T-SDLC-8-16.6, T-SDLC-8-17.6, T-SDLC-8-18.6, T-SDLC-8-19.6, T-SDLC-8-20.6, T-SDLC-8-21.6, T-SDLC-8-22.6, T-SDLC-8-23.6, T-SDLC-8-24.6, T-SDLC-8-25.6, T-SDLC-8-26.6, T-SDLC-8-27.6, T-SDLC-8-28.6, T-SDLC-9-2.6, T-SDLC-9-3.6, T-SDLC-9-4.6, T-SDLC-9-5.6, T-SDLC-9-6.6, T-SDLC-9-7.6, T-SDLC-9-8.6, T-SDLC-9-9.6, T-SDLC-9-10.6, T-SDLC-9-11.6, T-SDLC-9-12.6, T-SDLC-9-13.6, T-SDLC-9-14.6, T-SDLC-9-15.6, T-SDLC-9-16.6, T-SDLC-9-17.6, T-SDLC-10-2.6, T-SDLC-10-3.6, T-SDLC-10-4.6, T-SDLC-10-5.6, T-SDLC-10-6.6, T-SDLC-10-7.6, T-SDLC-10-8.6, T-SDLC-10-9.6, T-SDLC-10-10.6, T-SDLC-10-11.6, T-SDLC-10-12.6, T-SDLC-10-13.6, T-SDLC-10-14.6, T-SDLC-10-15.6, T-SDLC-10-16.6, T-SDLC-10-17.6, T-SDLC-11-1.6, T-SDLC-11-2.6, T-SDLC-11-3.6, T-SDLC-11-4.6, T-SDLC-11-5.6, T-SDLC-11-6.6, T-SDLC-11-7.6, T-SDLC-11-8.6, T-SDLC-11-9.6, T-SDLC-11-10.6, T-SDLC-11-11.6, T-SDLC-11-12.6, T-SDLC-11-13.6, T-SDLC-11-14.6, T-SDLC-12-2.6, T-SDLC-12-3.6, T-SDLC-12-4.6, T-SDLC-12-5.6, T-SDLC-12-6.6, T-SDLC-12-7.6, T-SDLC-12-8.6, T-SDLC-12-9.6, T-SDLC-12-10.6, T-SDLC-12-11.6, T-SDLC-12-12.6, T-SDLC-13-1.6, T-SDLC-13-2.6, T-SDLC-13-3.6, T-SDLC-13-4.6, T-SDLC-13-5.6, T-SDLC-13-6.6, T-SDLC-13-7.6, T-SDLC-13-8.6, T-SDLC-13-9.6, T-SDLC-13-10.6, T-SDLC-13-11.6, T-SDLC-13-12.6, T-SDLC-13-13.6, T-SDLC-14-1.6, T-SDLC-14-2.6, T-SDLC-14-3.6, T-SDLC-14-4.6, T-SDLC-14-5.6, T-SDLC-14-6.6, T-SDLC-14-7.6, T-SDLC-14-8.6, T-SDLC-14-9.6, T-SDLC-14-10.6, T-SDLC-14-11.6, T-SDLC-14-12.6, T-SDLC-14-13.6, T-SDLC-14-14.6, T-SDLC-15-2.6, T-SDLC-15-3.6, T-SDLC-15-4.6, T-SDLC-15-5.6, T-SDLC-15-6.6, T-SDLC-15-7.6, T-SDLC-15-8.6, T-SDLC-15-9.6, T-SDLC-15-10.6, T-SDLC-15-11.6, T-SDLC-15-12.6, T-SDLC-15-13.6, T-SDLC-15-14.6, T-SDLC-15-15.6, T-SDLC-16-1.6, T-SDLC-16-2.6, T-SDLC-16-3.6, T-SDLC-16-4.6, T-SDLC-16-5.6, T-SDLC-16-6.6, T-SDLC-16-7.6, T-SDLC-16-8.6, T-SDLC-16-9.6, T-SDLC-16-10.6, T-SDLC-16-11.6, T-SDLC-16-12.6, T-INT-HOST-02.6, T-INT-HOST-03.6, T-INT-HOST-04.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [All 257 original product tasks are accepted at their documented boundaries and every complete-product capability has composed verification; both AWS profiles are qualified independently, one selected profile serves the production domain; capability omissions block full completion rather than silently narrow scope; replacement migration/retirement remains separately authorized.]

- [ ] T-PROD.4 [Build immutable production release candidate]
  Stage: implement
  canonical-id: T-PROD.4
  authored-stage: implement
  deps: [T-PROD.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [A versioned release candidate is built from fetched accepted main with immutable artifact digest, source SHA, dependency/SBOM/license provenance, migration checksums, compatibility matrix and no production secrets; required release CI passes.]

- [ ] T-PROD.5 [Deploy and qualify staging on selected AWS profile]
  Stage: implement
  canonical-id: T-PROD.5
  authored-stage: implement
  deps: [T-PROD.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Reviewed Go Pulumi preview and staging rollout use scoped infrastructure authority; real AWS runtime, Cloudflare DNS/TLS, readiness, database, mail, identity, billing and interface checks bind to candidate digest; only approved test resources/provider modes are used; staging evidence does not claim production.]

- [ ] T-PROD.6 [Verify full staged application and recovery]
  Stage: verify
  canonical-id: T-PROD.6
  authored-stage: verify
  deps: [T-PROD.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Full positive/denied identity, organization, flat/seat/usage billing, business route, REST/MCP, upgrade and maintenance matrices pass in required real environments; tenant isolation, webhook retry, revocation, load/security checks and rollback/independent restore drills meet owner-selected RPO/RTO and capacity targets; both-profile evidence remains current.]

- [ ] T-PROD.7 [Independently review production release and change packet]
  Stage: review
  canonical-id: T-PROD.7
  authored-stage: review
  deps: [T-PROD.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent GPT-6-Luna reviewer records candidate source/base/head/digest, all included code coverage, actual CI/provider/staging/recovery evidence, Pulumi preview, permissions and public-safe runbooks; accepted findings create fix/verify/re-review tasks and invalidate affected release evidence; no blocking findings remain.]

- [ ] T-PROD.8 [Publish reviewed immutable release]
  Stage: implement
  canonical-id: T-PROD.8
  authored-stage: implement
  deps: [T-PROD.7]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Maintainer publishes the exact reviewed version and digest with truthful full capability evidence and reproducible operator instructions; release receipt is recorded. Any new code or infrastructure change returns through its own preflight/implement/verify/review/merge/verify-landed chain before rebuilding.]

- [ ] T-PROD.9 [Validate concrete production rollout]
  Stage: preflight
  canonical-id: T-PROD.9
  authored-stage: preflight
  deps: [T-PROD.8, T-PROD.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact amos.sire.run installation, selected AWS profile, budget, current Go Pulumi preview, DNS/TLS plan, secret references, least-privilege deployment identity, backup, migration and rollback procedures are validated against the reviewed digest; authority covers the concrete resources/actions and unresolved gates block rollout only.]

- [ ] T-REPLACEMENT-ACCEPT [Accept complete original and additive replacement delivery]
  Stage: verify-landed
  canonical-id: T-REPLACEMENT-ACCEPT
  authored-stage: accept
  deps: [T-PROD.13, T-RPL-REHEARSAL.6, T-FEATURE-EVOLUTION.6, T-OPTIONAL-CONTEXT.6, T-RPL-LOGIN-FRESHNESS.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Original full product and production terminal plus every added replacement, feature-evolution and optional-context branch have independently reviewed, landed and qualified evidence at their explicit boundaries. No narrative labels or baseline rehearsal replace full release qualification; consumer cutover still requires separate authorization.]

- [x] T-RPL-CONTRACT.1 [Adopt bounded standalone continuity acceptance and pure domain contract: preflight]
  Stage: preflight
  canonical-id: T-RPL-CONTRACT.1
  authored-stage: preflight
  deps: []
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [x] T-RPL-CONTRACT.2 [Adopt bounded standalone continuity acceptance and pure domain contract: author]
  Stage: implement
  canonical-id: T-RPL-CONTRACT.2
  authored-stage: author
  deps: [T-RPL-CONTRACT.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [x] T-RPL-CONTRACT.3 [Adopt bounded standalone continuity acceptance and pure domain contract: verify]
  Stage: verify
  canonical-id: T-RPL-CONTRACT.3
  authored-stage: verify
  deps: [T-RPL-CONTRACT.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [x] T-RPL-CONTRACT.4 [Adopt bounded standalone continuity acceptance and pure domain contract: review]
  Stage: review
  canonical-id: T-RPL-CONTRACT.4
  authored-stage: review
  deps: [T-RPL-CONTRACT.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [x] T-RPL-CONTRACT.5 [Adopt bounded standalone continuity acceptance and pure domain contract: merge]
  Stage: merge
  canonical-id: T-RPL-CONTRACT.5
  authored-stage: merge
  deps: [T-RPL-CONTRACT.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [x] T-RPL-CONTRACT.6 [Adopt bounded standalone continuity acceptance and pure domain contract: landed]
  Stage: verify-landed
  canonical-id: T-RPL-CONTRACT.6
  authored-stage: landed
  deps: [T-RPL-CONTRACT.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact public-safe contract and acceptance mapping independently approved; no source, service or replacement acceptance inferred. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-RPL-DATA.1 [Implement continuity application repository: preflight]
  Stage: preflight
  canonical-id: T-RPL-DATA.1
  authored-stage: preflight
  deps: [T-RPL-DOMAIN.6, T-RPL-REPOSITORY-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [x] T-RPL-DATA.2 [Implement continuity application repository: author]
  Stage: implement
  canonical-id: T-RPL-DATA.2
  authored-stage: author
  deps: [T-RPL-DATA.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [x] T-RPL-DATA.3 [Implement continuity application repository: verify]
  Stage: verify
  canonical-id: T-RPL-DATA.3
  authored-stage: verify
  deps: [T-RPL-DATA.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [x] T-RPL-DATA.4 [Implement continuity application repository: review]
  Stage: review
  canonical-id: T-RPL-DATA.4
  authored-stage: review
  deps: [T-RPL-DATA.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [x] T-RPL-DATA.5 [Implement continuity application repository: merge]
  Stage: merge
  canonical-id: T-RPL-DATA.5
  authored-stage: merge
  deps: [T-RPL-DATA.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [x] T-RPL-DATA.6 [Implement continuity application repository: landed]
  Stage: verify-landed
  canonical-id: T-RPL-DATA.6
  authored-stage: landed
  deps: [T-RPL-DATA.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Real PostgreSQL proves scoped register, case CAS, unsent draft, supporting checklist, procedure plus atomic activity, rollback and stale/foreign denial; app-owned migrations only under integrator allocation. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-RPL-DOMAIN.1 [Implement continuity domain value rules: preflight]
  Stage: preflight
  canonical-id: T-RPL-DOMAIN.1
  authored-stage: preflight
  deps: [T-RPL-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [x] T-RPL-DOMAIN.2 [Implement continuity domain value rules: author]
  Stage: implement
  canonical-id: T-RPL-DOMAIN.2
  authored-stage: author
  deps: [T-RPL-DOMAIN.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [x] T-RPL-DOMAIN.3 [Implement continuity domain value rules: verify]
  Stage: verify
  canonical-id: T-RPL-DOMAIN.3
  authored-stage: verify
  deps: [T-RPL-DOMAIN.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [x] T-RPL-DOMAIN.4 [Implement continuity domain value rules: review]
  Stage: review
  canonical-id: T-RPL-DOMAIN.4
  authored-stage: review
  deps: [T-RPL-DOMAIN.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [x] T-RPL-DOMAIN.5 [Implement continuity domain value rules: merge]
  Stage: merge
  canonical-id: T-RPL-DOMAIN.5
  authored-stage: merge
  deps: [T-RPL-DOMAIN.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [x] T-RPL-DOMAIN.6 [Implement continuity domain value rules: landed]
  Stage: verify-landed
  canonical-id: T-RPL-DOMAIN.6
  authored-stage: landed
  deps: [T-RPL-DOMAIN.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Finite validated case changes, unsent drafts, supporting checklist and procedure revision rules pass meaningful negatives; no I/O or authority API. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-RPL-GUIDE.1 [Implement explicit deterministic continuity scenarios: preflight]
  Stage: preflight
  canonical-id: T-RPL-GUIDE.1
  authored-stage: preflight
  deps: [T-RPL-DOMAIN.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [x] T-RPL-GUIDE.2 [Implement explicit deterministic continuity scenarios: author]
  Stage: implement
  canonical-id: T-RPL-GUIDE.2
  authored-stage: author
  deps: [T-RPL-GUIDE.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [x] T-RPL-GUIDE.3 [Implement explicit deterministic continuity scenarios: verify]
  Stage: verify
  canonical-id: T-RPL-GUIDE.3
  authored-stage: verify
  deps: [T-RPL-GUIDE.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [x] T-RPL-GUIDE.4 [Implement explicit deterministic continuity scenarios: review]
  Stage: review
  canonical-id: T-RPL-GUIDE.4
  authored-stage: review
  deps: [T-RPL-GUIDE.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [x] T-RPL-GUIDE.5 [Implement explicit deterministic continuity scenarios: merge]
  Stage: merge
  canonical-id: T-RPL-GUIDE.5
  authored-stage: merge
  deps: [T-RPL-GUIDE.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [x] T-RPL-GUIDE.6 [Implement explicit deterministic continuity scenarios: landed]
  Stage: verify-landed
  canonical-id: T-RPL-GUIDE.6
  authored-stage: landed
  deps: [T-RPL-GUIDE.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Pure guided scenarios use supplied current state and source references, preserve unknown authority and distinguish unsupported questions; no claims of live agents or effects. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [x] T-RPL-HOST-CONTRACT.1 [Adopt local continuity persistence and host authority contract: preflight]
  Stage: preflight
  canonical-id: T-RPL-HOST-CONTRACT.1
  authored-stage: preflight
  deps: [T-RPL-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: COMPLETE
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-RPL-HOST-CONTRACT.2 [Adopt local continuity persistence and host authority contract: author]
  Stage: implement
  canonical-id: T-RPL-HOST-CONTRACT.2
  authored-stage: author
  deps: [T-RPL-HOST-CONTRACT.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-RPL-HOST-CONTRACT.3 [Adopt local continuity persistence and host authority contract: verify]
  Stage: verify
  canonical-id: T-RPL-HOST-CONTRACT.3
  authored-stage: verify
  deps: [T-RPL-HOST-CONTRACT.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-RPL-HOST-CONTRACT.4 [Adopt local continuity persistence and host authority contract: review]
  Stage: review
  canonical-id: T-RPL-HOST-CONTRACT.4
  authored-stage: review
  deps: [T-RPL-HOST-CONTRACT.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]

- [ ] T-RPL-HOST-CONTRACT.5 [Adopt local continuity persistence and host authority contract: merge]
  Stage: merge
  canonical-id: T-RPL-HOST-CONTRACT.5
  authored-stage: merge
  deps: [T-RPL-HOST-CONTRACT.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Read back exact head/base/claim and approved review; guarded rebase merge respects protections. No auto-merge during active review.]

- [ ] T-RPL-HOST-CONTRACT.6 [Adopt local continuity persistence and host authority contract: landed]
  Stage: verify-landed
  canonical-id: T-RPL-HOST-CONTRACT.6
  authored-stage: landed
  deps: [T-RPL-HOST-CONTRACT.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Exact existing local-host/session/workspace/CSRF seams, current-state rechecks, SQL ownership and isolated migration allocation reviewed before dependent source; unresolved authority blocks composition. Verify merged source identity and fresh relevant checks; record obtained evidence and limitations. Narrative completion alone grants no acceptance.]

- [ ] T-RPL-HOST.1 [Compose independently runnable local continuity app: preflight]
  Stage: preflight
  canonical-id: T-RPL-HOST.1
  authored-stage: preflight
  deps: [T-RPL-WEB.6, T-RPL-HOST-CONTRACT.6]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Qualified existing local host is explicitly reused; actual signup/verify/signin/personal scope/CSRF/revocation/DB failure checks pass; standalone baseline has no paid or external-agent dependency. Reconcile exact prerequisites, claims, existing work and scope; absent required inputs block.]

- [ ] T-RPL-HOST.2 [Compose independently runnable local continuity app: author]
  Stage: implement
  canonical-id: T-RPL-HOST.2
  authored-stage: author
  deps: [T-RPL-HOST.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Qualified existing local host is explicitly reused; actual signup/verify/signin/personal scope/CSRF/revocation/DB failure checks pass; standalone baseline has no paid or external-agent dependency. Author the bounded deliverable and documentation without modifying sibling-owned paths.]

- [ ] T-RPL-HOST.3 [Compose independently runnable local continuity app: verify]
  Stage: verify
  canonical-id: T-RPL-HOST.3
  authored-stage: verify
  deps: [T-RPL-HOST.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Qualified existing local host is explicitly reused; actual signup/verify/signin/personal scope/CSRF/revocation/DB failure checks pass; standalone baseline has no paid or external-agent dependency. Run relevant real-service checks and meaningful isolated negative/restored checks; record exact commands and head.]

- [ ] T-RPL-HOST.4 [Compose independently runnable local continuity app: review]
  Stage: review
  canonical-id: T-RPL-HOST.4
  authored-stage: review
  deps: [T-RPL-HOST.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Qualified existing local host is explicitly reused; actual signup/verify/signin/personal scope/CSRF/revocation/DB failure checks pass; standalone baseline has no paid or external-agent dependency. Different author independently reviews exact head, repeats relevant checks and negative; findings require fix and re-review before clearance.]
