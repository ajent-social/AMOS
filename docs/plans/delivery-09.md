# Delivery inventory 09

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
Registry: `docs/planning/execution-state.json`; `sha256:d0ff86f9690ea28311feac6d27cc8e0c44aced1f42e6dd41307e639a235e84a4`.
Registry: `docs/planning/sdlc-stage-state.json`; `sha256:6ae4b0594209343bcb0f27305da70a96d562807833073626eba0d21932269004`.
See [projection contract](../planning/portable-plan-export.md) for regeneration and limits.
<!-- current-native-projection:end -->

- [ ] T-SDLC-3-22.6 [Verify landed T3.22 — Qualify and upstream PKCE support in AMSL OIDC]
  Stage: verify-landed
  canonical-id: T-SDLC-3-22.6
  authored-stage: verify-landed
  deps: [T-SDLC-3-22.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-3-23.1 [Preflight T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: preflight
  canonical-id: T-SDLC-3-23.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-3-20.6, T-SDLC-10-10.6, T-SDLC-9-12.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T3.20, T10.10, T9.12 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-3-23.2 [Implement T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: implement
  canonical-id: T-SDLC-3-23.2
  authored-stage: implement
  deps: [T-SDLC-3-23.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Restored old session or removed credential cannot authenticate after recovery activation. Unknown future credential/session schema blocks unsafe downgrade with actionable error. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-3-23.3 [Verify changed behavior and required checks T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: verify
  canonical-id: T-SDLC-3-23.3
  authored-stage: verify
  deps: [T-SDLC-3-23.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-3-23.4 [Independently review T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: review
  canonical-id: T-SDLC-3-23.4
  authored-stage: review
  deps: [T-SDLC-3-23.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T3.23 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-3-23.5 [Rebase merge T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: merge
  canonical-id: T-SDLC-3-23.5
  authored-stage: merge
  deps: [T-SDLC-3-23.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-3-23.6 [Verify landed T3.23 — Add identity restore epoch and security upgrade compatibility probe]
  Stage: verify-landed
  canonical-id: T-SDLC-3-23.6
  authored-stage: verify-landed
  deps: [T-SDLC-3-23.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-10.1 [Preflight T4.10 — Implement organization OIDC login and enforcement]
  Stage: preflight
  canonical-id: T-SDLC-4-10.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-9.6, T-SDLC-3-22.6, T-SDLC-3-17.6, T-SDLC-4-8.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.9, T3.22, T3.17, T4.8 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-10.2 [Implement T4.10 — Implement organization OIDC login and enforcement]
  Stage: implement
  canonical-id: T-SDLC-4-10.2
  authored-stage: implement
  deps: [T-SDLC-4-10.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [SSO proof from one organization cannot satisfy another organization policy. Existing low-assurance session receives resumable proof challenge after enforcement starts. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-10.3 [Verify changed behavior and required checks T4.10 — Implement organization OIDC login and enforcement]
  Stage: verify
  canonical-id: T-SDLC-4-10.3
  authored-stage: verify
  deps: [T-SDLC-4-10.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-10.4 [Independently review T4.10 — Implement organization OIDC login and enforcement]
  Stage: review
  canonical-id: T-SDLC-4-10.4
  authored-stage: review
  deps: [T-SDLC-4-10.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.10 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-10.5 [Rebase merge T4.10 — Implement organization OIDC login and enforcement]
  Stage: merge
  canonical-id: T-SDLC-4-10.5
  authored-stage: merge
  deps: [T-SDLC-4-10.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-10.6 [Verify landed T4.10 — Implement organization OIDC login and enforcement]
  Stage: verify-landed
  canonical-id: T-SDLC-4-10.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-10.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-11.1 [Preflight T4.11 — Implement organization security policy and MFA rollout]
  Stage: preflight
  canonical-id: T-SDLC-4-11.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-8.6, T-SDLC-3-17.6, T-SDLC-4-10.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.8, T3.17, T4.10 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-11.2 [Implement T4.11 — Implement organization security policy and MFA rollout]
  Stage: implement
  canonical-id: T-SDLC-4-11.2
  authored-stage: implement
  deps: [T-SDLC-4-11.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Enabling MFA requires owner to possess viable qualifying factors and recovery path. Noncompliant members cannot execute protected business action until policy proof succeeds. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-11.3 [Verify changed behavior and required checks T4.11 — Implement organization security policy and MFA rollout]
  Stage: verify
  canonical-id: T-SDLC-4-11.3
  authored-stage: verify
  deps: [T-SDLC-4-11.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-11.4 [Independently review T4.11 — Implement organization security policy and MFA rollout]
  Stage: review
  canonical-id: T-SDLC-4-11.4
  authored-stage: review
  deps: [T-SDLC-4-11.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.11 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-11.5 [Rebase merge T4.11 — Implement organization security policy and MFA rollout]
  Stage: merge
  canonical-id: T-SDLC-4-11.5
  authored-stage: merge
  deps: [T-SDLC-4-11.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-11.6 [Verify landed T4.11 — Implement organization security policy and MFA rollout]
  Stage: verify-landed
  canonical-id: T-SDLC-4-11.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-11.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-12.1 [Preflight T4.12 — Implement leave, removal and ownership transfer]
  Stage: preflight
  canonical-id: T-SDLC-4-12.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-8.6, T-SDLC-4-7.6, T-SDLC-3-17.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.8, T4.7, T3.17 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-12.2 [Implement T4.12 — Implement leave, removal and ownership transfer]
  Stage: implement
  canonical-id: T-SDLC-4-12.2
  authored-stage: implement
  deps: [T-SDLC-4-12.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [No operation sequence can leave active organization without eligible owner. Removed member loses authority before delayed seat-credit/provider work completes. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-12.3 [Verify changed behavior and required checks T4.12 — Implement leave, removal and ownership transfer]
  Stage: verify
  canonical-id: T-SDLC-4-12.3
  authored-stage: verify
  deps: [T-SDLC-4-12.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-12.4 [Independently review T4.12 — Implement leave, removal and ownership transfer]
  Stage: review
  canonical-id: T-SDLC-4-12.4
  authored-stage: review
  deps: [T-SDLC-4-12.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.12 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-12.5 [Rebase merge T4.12 — Implement leave, removal and ownership transfer]
  Stage: merge
  canonical-id: T-SDLC-4-12.5
  authored-stage: merge
  deps: [T-SDLC-4-12.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-12.6 [Verify landed T4.12 — Implement leave, removal and ownership transfer]
  Stage: verify-landed
  canonical-id: T-SDLC-4-12.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-12.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-13.1 [Preflight T4.13 — Implement organization suspension, export and deletion]
  Stage: preflight
  canonical-id: T-SDLC-4-13.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-12.6, T-SDLC-5-17.6, T1.10, T-SDLC-1-13.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.12, T5.17, T1.10, T1.13 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-13.2 [Implement T4.13 — Implement organization suspension, export and deletion]
  Stage: implement
  canonical-id: T-SDLC-4-13.2
  authored-stage: implement
  deps: [T-SDLC-4-13.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Suspension denies business access while limited account/billing recovery routes remain available. Deletion completes only when all registered resource cleanup participants and retained billing policy agree. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-13.3 [Verify changed behavior and required checks T4.13 — Implement organization suspension, export and deletion]
  Stage: verify
  canonical-id: T-SDLC-4-13.3
  authored-stage: verify
  deps: [T-SDLC-4-13.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-13.4 [Independently review T4.13 — Implement organization suspension, export and deletion]
  Stage: review
  canonical-id: T-SDLC-4-13.4
  authored-stage: review
  deps: [T-SDLC-4-13.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.13 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-13.5 [Rebase merge T4.13 — Implement organization suspension, export and deletion]
  Stage: merge
  canonical-id: T-SDLC-4-13.5
  authored-stage: merge
  deps: [T-SDLC-4-13.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-13.6 [Verify landed T4.13 — Implement organization suspension, export and deletion]
  Stage: verify-landed
  canonical-id: T-SDLC-4-13.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-13.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-14.1 [Preflight T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: preflight
  canonical-id: T-SDLC-4-14.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-11.6, T-SDLC-4-12.6, T-SDLC-4-13.6, T1.12, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.11, T4.12, T4.13, T1.12 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-14.2 [Implement T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: implement
  canonical-id: T-SDLC-4-14.2
  authored-stage: implement
  deps: [T-SDLC-4-14.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [All selected administration changes emit attributable sanitized audit events. Organization audit API cannot reveal another tenant even with copied cursor. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-14.3 [Verify changed behavior and required checks T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: verify
  canonical-id: T-SDLC-4-14.3
  authored-stage: verify
  deps: [T-SDLC-4-14.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-14.4 [Independently review T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: review
  canonical-id: T-SDLC-4-14.4
  authored-stage: review
  deps: [T-SDLC-4-14.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.14 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-14.5 [Rebase merge T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: merge
  canonical-id: T-SDLC-4-14.5
  authored-stage: merge
  deps: [T-SDLC-4-14.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-14.6 [Verify landed T4.14 — Expose tenant-safe administration audit and pagination]
  Stage: verify-landed
  canonical-id: T-SDLC-4-14.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-14.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-5.1 [Preflight T4.5 — Implement organization creation and profile administration]
  Stage: preflight
  canonical-id: T-SDLC-4-5.1
  authored-stage: preflight
  deps: [T-PROD.1, T4.2, T4.4, T-SDLC-3-17.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.2, T4.4, T3.17 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-5.2 [Implement T4.5 — Implement organization creation and profile administration]
  Stage: implement
  canonical-id: T-SDLC-4-5.2
  authored-stage: implement
  deps: [T-SDLC-4-5.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Organization creator becomes sole initial owner and personal workspace remains distinct. Member cannot alter organization administration fields. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-5.3 [Verify changed behavior and required checks T4.5 — Implement organization creation and profile administration]
  Stage: verify
  canonical-id: T-SDLC-4-5.3
  authored-stage: verify
  deps: [T-SDLC-4-5.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-5.4 [Independently review T4.5 — Implement organization creation and profile administration]
  Stage: review
  canonical-id: T-SDLC-4-5.4
  authored-stage: review
  deps: [T-SDLC-4-5.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.5 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-5.5 [Rebase merge T4.5 — Implement organization creation and profile administration]
  Stage: merge
  canonical-id: T-SDLC-4-5.5
  authored-stage: merge
  deps: [T-SDLC-4-5.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-5.6 [Verify landed T4.5 — Implement organization creation and profile administration]
  Stage: verify-landed
  canonical-id: T-SDLC-4-5.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-5.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-6.1 [Preflight T4.6 — Implement invitation issue, resend and revocation]
  Stage: preflight
  canonical-id: T-SDLC-4-6.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-5.6, T3.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.5, T3.6 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-6.2 [Implement T4.6 — Implement invitation issue, resend and revocation]
  Stage: implement
  canonical-id: T-SDLC-4-6.2
  authored-stage: implement
  deps: [T-SDLC-4-6.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Only authorized inviter can issue permitted role, and email intent commits atomically. Revoking invitation prevents future acceptance without affecting existing membership. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-6.3 [Verify changed behavior and required checks T4.6 — Implement invitation issue, resend and revocation]
  Stage: verify
  canonical-id: T-SDLC-4-6.3
  authored-stage: verify
  deps: [T-SDLC-4-6.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-6.4 [Independently review T4.6 — Implement invitation issue, resend and revocation]
  Stage: review
  canonical-id: T-SDLC-4-6.4
  authored-stage: review
  deps: [T-SDLC-4-6.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.6 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-6.5 [Rebase merge T4.6 — Implement invitation issue, resend and revocation]
  Stage: merge
  canonical-id: T-SDLC-4-6.5
  authored-stage: merge
  deps: [T-SDLC-4-6.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-6.6 [Verify landed T4.6 — Implement invitation issue, resend and revocation]
  Stage: verify-landed
  canonical-id: T-SDLC-4-6.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-6.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-7.1 [Preflight T4.7 — Implement invitation acceptance with seat reservation]
  Stage: preflight
  canonical-id: T-SDLC-4-7.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-6.6, T-SDLC-5-14.6, T-SDLC-3-17.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.6, T5.14, T3.17 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-7.2 [Implement T4.7 — Implement invitation acceptance with seat reservation]
  Stage: implement
  canonical-id: T-SDLC-4-7.2
  authored-stage: implement
  deps: [T-SDLC-4-7.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [One accepted invite yields one membership and one counted seat reservation. Unknown provider outcome cannot silently grant seat access or charge twice. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-7.3 [Verify changed behavior and required checks T4.7 — Implement invitation acceptance with seat reservation]
  Stage: verify
  canonical-id: T-SDLC-4-7.3
  authored-stage: verify
  deps: [T-SDLC-4-7.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-7.4 [Independently review T4.7 — Implement invitation acceptance with seat reservation]
  Stage: review
  canonical-id: T-SDLC-4-7.4
  authored-stage: review
  deps: [T-SDLC-4-7.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.7 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-7.5 [Rebase merge T4.7 — Implement invitation acceptance with seat reservation]
  Stage: merge
  canonical-id: T-SDLC-4-7.5
  authored-stage: merge
  deps: [T-SDLC-4-7.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-7.6 [Verify landed T4.7 — Implement invitation acceptance with seat reservation]
  Stage: verify-landed
  canonical-id: T-SDLC-4-7.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-7.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-8.1 [Preflight T4.8 — Implement roles and application permission vocabulary]
  Stage: preflight
  canonical-id: T-SDLC-4-8.1
  authored-stage: preflight
  deps: [T-PROD.1, T4.4, T-SDLC-4-5.6, T1.2, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.4, T4.5, T1.2 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-8.2 [Implement T4.8 — Implement roles and application permission vocabulary]
  Stage: implement
  canonical-id: T-SDLC-4-8.2
  authored-stage: implement
  deps: [T-SDLC-4-8.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Unknown permission denies; both tenant authority and entitlement are required for paid actions. API/MCP grants cannot exceed current membership permissions. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-8.3 [Verify changed behavior and required checks T4.8 — Implement roles and application permission vocabulary]
  Stage: verify
  canonical-id: T-SDLC-4-8.3
  authored-stage: verify
  deps: [T-SDLC-4-8.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-8.4 [Independently review T4.8 — Implement roles and application permission vocabulary]
  Stage: review
  canonical-id: T-SDLC-4-8.4
  authored-stage: review
  deps: [T-SDLC-4-8.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.8 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-8.5 [Rebase merge T4.8 — Implement roles and application permission vocabulary]
  Stage: merge
  canonical-id: T-SDLC-4-8.5
  authored-stage: merge
  deps: [T-SDLC-4-8.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-8.6 [Verify landed T4.8 — Implement roles and application permission vocabulary]
  Stage: verify-landed
  canonical-id: T-SDLC-4-8.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-8.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-4-9.1 [Preflight T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: preflight
  canonical-id: T-SDLC-4-9.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-4-5.6, T3.10, T-SDLC-3-17.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T4.5, T3.10, T3.17 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-4-9.2 [Implement T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: implement
  canonical-id: T-SDLC-4-9.2
  authored-stage: implement
  deps: [T-SDLC-4-9.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Organization A cannot read/change B IdP configuration or receive its issuer identity binding. Discovery requests cannot access disallowed private/metadata endpoints. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-4-9.3 [Verify changed behavior and required checks T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: verify
  canonical-id: T-SDLC-4-9.3
  authored-stage: verify
  deps: [T-SDLC-4-9.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-4-9.4 [Independently review T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: review
  canonical-id: T-SDLC-4-9.4
  authored-stage: review
  deps: [T-SDLC-4-9.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T4.9 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-4-9.5 [Rebase merge T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: merge
  canonical-id: T-SDLC-4-9.5
  authored-stage: merge
  deps: [T-SDLC-4-9.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-4-9.6 [Verify landed T4.9 — Implement per-organization OIDC configuration and domain proof]
  Stage: verify-landed
  canonical-id: T-SDLC-4-9.6
  authored-stage: verify-landed
  deps: [T-SDLC-4-9.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-10.1 [Preflight T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: preflight
  canonical-id: T-SDLC-5-10.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-5-9.6, T-SDLC-6-5.6, T-SDLC-8-17.6, T-SDLC-8-28.6, T-SDLC-7-11.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.9, T6.5, T8.17, T8.28, T7.11 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-10.2 [Implement T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: implement
  canonical-id: T-SDLC-5-10.2
  authored-stage: implement
  deps: [T-SDLC-5-10.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Observed provider test payment controls actual deployed test entitlement, with duplicate/delay recovery evidence. No fixture response is counted as actual hosted-checkout or live-payment evidence. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-10.3 [Verify changed behavior and required checks T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: verify
  canonical-id: T-SDLC-5-10.3
  authored-stage: verify
  deps: [T-SDLC-5-10.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-5-10.4 [Independently review T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: review
  canonical-id: T-SDLC-5-10.4
  authored-stage: review
  deps: [T-SDLC-5-10.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T5.10 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-5-10.5 [Rebase merge T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: merge
  canonical-id: T-SDLC-5-10.5
  authored-stage: merge
  deps: [T-SDLC-5-10.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-5-10.6 [Verify landed T5.10 — Qualify real Stripe test-mode payment journey]
  Stage: verify-landed
  canonical-id: T-SDLC-5-10.6
  authored-stage: verify-landed
  deps: [T-SDLC-5-10.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-11.1 [Preflight T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: preflight
  canonical-id: T-SDLC-5-11.1
  authored-stage: preflight
  deps: [T-PROD.1, T5.8, T-SDLC-3-17.6, T-SDLC-4-8.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.8, T3.17, T4.8 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-11.2 [Implement T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: implement
  canonical-id: T-SDLC-5-11.2
  authored-stage: implement
  deps: [T-SDLC-5-11.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Only billing-authorized actor can create portal/manage subscription; expired proof returns challenge. Unknown or payment-action-required plan change cannot claim activated entitlement. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-11.3 [Verify changed behavior and required checks T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: verify
  canonical-id: T-SDLC-5-11.3
  authored-stage: verify
  deps: [T-SDLC-5-11.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-5-11.4 [Independently review T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: review
  canonical-id: T-SDLC-5-11.4
  authored-stage: review
  deps: [T-SDLC-5-11.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T5.11 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-5-11.5 [Rebase merge T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: merge
  canonical-id: T-SDLC-5-11.5
  authored-stage: merge
  deps: [T-SDLC-5-11.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-5-11.6 [Verify landed T5.11 — Implement portal and subscription lifecycle APIs]
  Stage: verify-landed
  canonical-id: T-SDLC-5-11.6
  authored-stage: verify-landed
  deps: [T-SDLC-5-11.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-12.1 [Preflight T5.12 — Add invoice, refund and dispute state normalization]
  Stage: preflight
  canonical-id: T-SDLC-5-12.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-5-11.6, T5.1, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.11, T5.1 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-12.2 [Implement T5.12 — Add invoice, refund and dispute state normalization]
  Stage: implement
  canonical-id: T-SDLC-5-12.2
  authored-stage: implement
  deps: [T-SDLC-5-12.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Billing owner sees only own workspace invoice/history and actionable payment state. Refund/dispute impact matches accepted policy, not a hardcoded universal revoked/allowed rule. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-12.3 [Verify changed behavior and required checks T5.12 — Add invoice, refund and dispute state normalization]
  Stage: verify
  canonical-id: T-SDLC-5-12.3
  authored-stage: verify
  deps: [T-SDLC-5-12.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-5-12.4 [Independently review T5.12 — Add invoice, refund and dispute state normalization]
  Stage: review
  canonical-id: T-SDLC-5-12.4
  authored-stage: review
  deps: [T-SDLC-5-12.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T5.12 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-5-12.5 [Rebase merge T5.12 — Add invoice, refund and dispute state normalization]
  Stage: merge
  canonical-id: T-SDLC-5-12.5
  authored-stage: merge
  deps: [T-SDLC-5-12.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-5-12.6 [Verify landed T5.12 — Add invoice, refund and dispute state normalization]
  Stage: verify-landed
  canonical-id: T-SDLC-5-12.6
  authored-stage: verify-landed
  deps: [T-SDLC-5-12.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-13.1 [Preflight T5.13 — Implement seat quantity projection and reconciliation]
  Stage: preflight
  canonical-id: T-SDLC-5-13.1
  authored-stage: preflight
  deps: [T-PROD.1, T5.3, T5.8, T4.2, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.3, T5.8, T4.2 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-13.2 [Implement T5.13 — Implement seat quantity projection and reconciliation]
  Stage: implement
  canonical-id: T-SDLC-5-13.2
  authored-stage: implement
  deps: [T-SDLC-5-13.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Confirmed provider quantity eventually equals desired billable seats without older worker overriding newer membership version. Uncertain update retains pending state with attributable intent. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-13.3 [Verify changed behavior and required checks T5.13 — Implement seat quantity projection and reconciliation]
  Stage: verify
  canonical-id: T-SDLC-5-13.3
  authored-stage: verify
  deps: [T-SDLC-5-13.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-5-13.4 [Independently review T5.13 — Implement seat quantity projection and reconciliation]
  Stage: review
  canonical-id: T-SDLC-5-13.4
  authored-stage: review
  deps: [T-SDLC-5-13.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T5.13 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-5-13.5 [Rebase merge T5.13 — Implement seat quantity projection and reconciliation]
  Stage: merge
  canonical-id: T-SDLC-5-13.5
  authored-stage: merge
  deps: [T-SDLC-5-13.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-5-13.6 [Verify landed T5.13 — Implement seat quantity projection and reconciliation]
  Stage: verify-landed
  canonical-id: T-SDLC-5-13.6
  authored-stage: verify-landed
  deps: [T-SDLC-5-13.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-14.1 [Preflight T5.14 — Implement transactional seat reservations for membership changes]
  Stage: preflight
  canonical-id: T-SDLC-5-14.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-5-13.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.13 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-14.2 [Implement T5.14 — Implement transactional seat reservations for membership changes]
  Stage: implement
  canonical-id: T-SDLC-5-14.2
  authored-stage: implement
  deps: [T-SDLC-5-14.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Seat-limited model cannot oversubscribe accepted members under concurrent invite acceptance. Rolled-back membership does not leave permanent seat reservation or duplicate charge. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-14.3 [Verify changed behavior and required checks T5.14 — Implement transactional seat reservations for membership changes]
  Stage: verify
  canonical-id: T-SDLC-5-14.3
  authored-stage: verify
  deps: [T-SDLC-5-14.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-5-14.4 [Independently review T5.14 — Implement transactional seat reservations for membership changes]
  Stage: review
  canonical-id: T-SDLC-5-14.4
  authored-stage: review
  deps: [T-SDLC-5-14.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T5.14 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-5-14.5 [Rebase merge T5.14 — Implement transactional seat reservations for membership changes]
  Stage: merge
  canonical-id: T-SDLC-5-14.5
  authored-stage: merge
  deps: [T-SDLC-5-14.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-5-14.6 [Verify landed T5.14 — Implement transactional seat reservations for membership changes]
  Stage: verify-landed
  canonical-id: T-SDLC-5-14.6
  authored-stage: verify-landed
  deps: [T-SDLC-5-14.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-5-15.1 [Preflight T5.15 — Create immutable usage event ledger]
  Stage: preflight
  canonical-id: T-SDLC-5-15.1
  authored-stage: preflight
  deps: [T-PROD.1, T5.1, T5.3, T-SDLC-4-8.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T5.1, T5.3, T4.8 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-5-15.2 [Implement T5.15 — Create immutable usage event ledger]
  Stage: implement
  canonical-id: T-SDLC-5-15.2
  authored-stage: implement
  deps: [T-SDLC-5-15.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Same usage identity contributes once; differing payload under same identity conflicts. Failed rolled-back business operation cannot create finalized billable usage. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-5-15.3 [Verify changed behavior and required checks T5.15 — Create immutable usage event ledger]
  Stage: verify
  canonical-id: T-SDLC-5-15.3
  authored-stage: verify
  deps: [T-SDLC-5-15.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]
