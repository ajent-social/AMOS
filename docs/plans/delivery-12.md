# Delivery inventory 12

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
Registry: `docs/planning/execution-state.json`; `sha256:ede50e7f1d2f8fafb5ee83e155212389e91399ace4dbb2e3d4d3f1aff4f63348`.
Registry: `docs/planning/sdlc-stage-state.json`; `sha256:6ae4b0594209343bcb0f27305da70a96d562807833073626eba0d21932269004`.
See [projection contract](../planning/portable-plan-export.md) for regeneration and limits.
<!-- current-native-projection:end -->

- [ ] T-SDLC-8-11.6 [Verify landed T8.11 — Propose upstream infrastructure and workflow gaps]
  Stage: verify-landed
  canonical-id: T-SDLC-8-11.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-11.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-12.1 [Preflight T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: preflight
  canonical-id: T-SDLC-8-12.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-3.6, T-SDLC-8-5.6, T-SDLC-8-7.6, T-SDLC-8-9.6, T-SDLC-8-10.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.3, T8.5, T8.7, T8.9, T8.10 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-12.2 [Implement T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: implement
  canonical-id: T-SDLC-8-12.2
  authored-stage: implement
  deps: [T-SDLC-8-12.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Generated program composes with reviewed pins and needs no Pulumi Cloud account. Second mock evaluation has stable resource identities and does not expose secret values. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-12.3 [Verify changed behavior and required checks T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: verify
  canonical-id: T-SDLC-8-12.3
  authored-stage: verify
  deps: [T-SDLC-8-12.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-12.4 [Independently review T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: review
  canonical-id: T-SDLC-8-12.4
  authored-stage: review
  deps: [T-SDLC-8-12.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.12 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-12.5 [Rebase merge T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: merge
  canonical-id: T-SDLC-8-12.5
  authored-stage: merge
  deps: [T-SDLC-8-12.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-12.6 [Verify landed T8.12 — Small-VM: Compose the AWS Pulumi program and outputs]
  Stage: verify-landed
  canonical-id: T-SDLC-8-12.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-12.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-13.1 [Preflight T8.13 — Small-VM: Implement immutable host release activation]
  Stage: preflight
  canonical-id: T-SDLC-8-13.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-12.6, T-SDLC-9-5.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.12, T9.5 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-13.2 [Implement T8.13 — Small-VM: Implement immutable host release activation]
  Stage: implement
  canonical-id: T-SDLC-8-13.2
  authored-stage: implement
  deps: [T-SDLC-8-13.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Mutable image tags cannot be used as deployment approval. An invalid platform or signature blocks activation before current service stop. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-13.3 [Verify changed behavior and required checks T8.13 — Small-VM: Implement immutable host release activation]
  Stage: verify
  canonical-id: T-SDLC-8-13.3
  authored-stage: verify
  deps: [T-SDLC-8-13.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-13.4 [Independently review T8.13 — Small-VM: Implement immutable host release activation]
  Stage: review
  canonical-id: T-SDLC-8-13.4
  authored-stage: review
  deps: [T-SDLC-8-13.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.13 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-13.5 [Rebase merge T8.13 — Small-VM: Implement immutable host release activation]
  Stage: merge
  canonical-id: T-SDLC-8-13.5
  authored-stage: merge
  deps: [T-SDLC-8-13.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-13.6 [Verify landed T8.13 — Small-VM: Implement immutable host release activation]
  Stage: verify-landed
  canonical-id: T-SDLC-8-13.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-13.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-14.1 [Preflight T8.14 — Add resource, deletion and cost policies]
  Stage: preflight
  canonical-id: T-SDLC-8-14.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-22.6, T8.1, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.22, T8.1 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-14.2 [Implement T8.14 — Add resource, deletion and cost policies]
  Stage: implement
  canonical-id: T-SDLC-8-14.2
  authored-stage: implement
  deps: [T-SDLC-8-14.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Policy rejects the enumerated unsafe fixtures before apply. Cost notices include resources retained after teardown and variable-cost caveats. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-14.3 [Verify changed behavior and required checks T8.14 — Add resource, deletion and cost policies]
  Stage: verify
  canonical-id: T-SDLC-8-14.3
  authored-stage: verify
  deps: [T-SDLC-8-14.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-14.4 [Independently review T8.14 — Add resource, deletion and cost policies]
  Stage: review
  canonical-id: T-SDLC-8-14.4
  authored-stage: review
  deps: [T-SDLC-8-14.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.14 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-14.5 [Rebase merge T8.14 — Add resource, deletion and cost policies]
  Stage: merge
  canonical-id: T-SDLC-8-14.5
  authored-stage: merge
  deps: [T-SDLC-8-14.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-14.6 [Verify landed T8.14 — Add resource, deletion and cost policies]
  Stage: verify-landed
  canonical-id: T-SDLC-8-14.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-14.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-15.1 [Preflight T8.15 — Expose infrastructure preview and teardown planning]
  Stage: preflight
  canonical-id: T-SDLC-8-15.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-22.6, T-SDLC-8-14.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.22, T8.14 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-15.2 [Implement T8.15 — Expose infrastructure preview and teardown planning]
  Stage: implement
  canonical-id: T-SDLC-8-15.2
  authored-stage: implement
  deps: [T-SDLC-8-15.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [A failed preview cannot produce an approvable token. Default teardown plan retains state, recovery keys and configured database/backup resources. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-15.3 [Verify changed behavior and required checks T8.15 — Expose infrastructure preview and teardown planning]
  Stage: verify
  canonical-id: T-SDLC-8-15.3
  authored-stage: verify
  deps: [T-SDLC-8-15.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-15.4 [Independently review T8.15 — Expose infrastructure preview and teardown planning]
  Stage: review
  canonical-id: T-SDLC-8-15.4
  authored-stage: review
  deps: [T-SDLC-8-15.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.15 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-15.5 [Rebase merge T8.15 — Expose infrastructure preview and teardown planning]
  Stage: merge
  canonical-id: T-SDLC-8-15.5
  authored-stage: merge
  deps: [T-SDLC-8-15.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-15.6 [Verify landed T8.15 — Expose infrastructure preview and teardown planning]
  Stage: verify-landed
  canonical-id: T-SDLC-8-15.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-15.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-16.1 [Preflight T8.16 — Specify optional proxy mode qualification]
  Stage: preflight
  canonical-id: T-SDLC-8-16.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-8.6, T-SDLC-8-21.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.8, T8.21 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-16.2 [Implement T8.16 — Specify optional proxy mode qualification]
  Stage: implement
  canonical-id: T-SDLC-8-16.2
  authored-stage: implement
  deps: [T-SDLC-8-16.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Proxy mode remains unavailable unless its separate contract and live evidence are approved. Qualification includes cached authenticated-response and direct-origin bypass failures. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-16.3 [Verify changed behavior and required checks T8.16 — Specify optional proxy mode qualification]
  Stage: verify
  canonical-id: T-SDLC-8-16.3
  authored-stage: verify
  deps: [T-SDLC-8-16.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-16.4 [Independently review T8.16 — Specify optional proxy mode qualification]
  Stage: review
  canonical-id: T-SDLC-8-16.4
  authored-stage: review
  deps: [T-SDLC-8-16.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.16 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-16.5 [Rebase merge T8.16 — Specify optional proxy mode qualification]
  Stage: merge
  canonical-id: T-SDLC-8-16.5
  authored-stage: merge
  deps: [T-SDLC-8-16.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-16.6 [Verify landed T8.16 — Specify optional proxy mode qualification]
  Stage: verify-landed
  canonical-id: T-SDLC-8-16.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-16.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-17.1 [Preflight T8.17 — Prepare managed-profile cloud qualification]
  Stage: preflight
  canonical-id: T-SDLC-8-17.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-22.6, T-SDLC-8-14.6, T-SDLC-8-15.6, T-SDLC-10-10.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.22, T8.14, T8.15, T10.10 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-17.2 [Implement T8.17 — Prepare managed-profile cloud qualification]
  Stage: implement
  canonical-id: T-SDLC-8-17.2
  authored-stage: implement
  deps: [T-SDLC-8-17.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Default test invocation cannot create cloud resources. Live checklist names exact authorization inputs and cannot mark mock evidence as live. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-17.3 [Verify changed behavior and required checks T8.17 — Prepare managed-profile cloud qualification]
  Stage: verify
  canonical-id: T-SDLC-8-17.3
  authored-stage: verify
  deps: [T-SDLC-8-17.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-17.4 [Independently review T8.17 — Prepare managed-profile cloud qualification]
  Stage: review
  canonical-id: T-SDLC-8-17.4
  authored-stage: review
  deps: [T-SDLC-8-17.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.17 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-17.5 [Rebase merge T8.17 — Prepare managed-profile cloud qualification]
  Stage: merge
  canonical-id: T-SDLC-8-17.5
  authored-stage: merge
  deps: [T-SDLC-8-17.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-17.6 [Verify landed T8.17 — Prepare managed-profile cloud qualification]
  Stage: verify-landed
  canonical-id: T-SDLC-8-17.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-17.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-18.1 [Preflight T8.18 — Format and lint infrastructure packages and assets]
  Stage: preflight
  canonical-id: T-SDLC-8-18.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-17.6, T1.6, T1.8, T-SDLC-8-23.6, T-SDLC-8-28.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.17, T1.6, T1.8, T8.23, T8.28 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-18.2 [Implement T8.18 — Format and lint infrastructure packages and assets]
  Stage: implement
  canonical-id: T-SDLC-8-18.2
  authored-stage: implement
  deps: [T-SDLC-8-18.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [No malformed generated asset passes the contract fixture suite. Source qualification records distinguish static, mocked and live evidence. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-18.3 [Verify changed behavior and required checks T8.18 — Format and lint infrastructure packages and assets]
  Stage: verify
  canonical-id: T-SDLC-8-18.3
  authored-stage: verify
  deps: [T-SDLC-8-18.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-18.4 [Independently review T8.18 — Format and lint infrastructure packages and assets]
  Stage: review
  canonical-id: T-SDLC-8-18.4
  authored-stage: review
  deps: [T-SDLC-8-18.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.18 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-18.5 [Rebase merge T8.18 — Format and lint infrastructure packages and assets]
  Stage: merge
  canonical-id: T-SDLC-8-18.5
  authored-stage: merge
  deps: [T-SDLC-8-18.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-18.6 [Verify landed T8.18 — Format and lint infrastructure packages and assets]
  Stage: verify-landed
  canonical-id: T-SDLC-8-18.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-18.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-19.1 [Preflight T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: preflight
  canonical-id: T-SDLC-8-19.1
  authored-stage: preflight
  deps: [T-PROD.1, T8.1, T8.2, T1.4, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.1, T8.2, T1.4 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-19.2 [Implement T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: implement
  canonical-id: T-SDLC-8-19.2
  authored-stage: implement
  deps: [T-SDLC-8-19.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fargate service can be represented with a complete egress path and no public database rule. Task identity cannot provision resources or read state; execution and application permissions differ. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-19.3 [Verify changed behavior and required checks T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: verify
  canonical-id: T-SDLC-8-19.3
  authored-stage: verify
  deps: [T-SDLC-8-19.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-19.4 [Independently review T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: review
  canonical-id: T-SDLC-8-19.4
  authored-stage: review
  deps: [T-SDLC-8-19.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.19 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-19.5 [Rebase merge T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: merge
  canonical-id: T-SDLC-8-19.5
  authored-stage: merge
  deps: [T-SDLC-8-19.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-19.6 [Verify landed T8.19 — Managed: compose network, cluster and Fargate service]
  Stage: verify-landed
  canonical-id: T-SDLC-8-19.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-19.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-20.1 [Preflight T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: preflight
  canonical-id: T-SDLC-8-20.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-19.6, T1.4, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.19, T1.4 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-20.2 [Implement T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: implement
  canonical-id: T-SDLC-8-20.2
  authored-stage: implement
  deps: [T-SDLC-8-20.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Database PubliclyAccessible is false and encryption is enabled. No credentials enter stack outputs, generated source or CI artifacts. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-20.3 [Verify changed behavior and required checks T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: verify
  canonical-id: T-SDLC-8-20.3
  authored-stage: verify
  deps: [T-SDLC-8-20.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-20.4 [Independently review T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: review
  canonical-id: T-SDLC-8-20.4
  authored-stage: review
  deps: [T-SDLC-8-20.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.20 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-20.5 [Rebase merge T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: merge
  canonical-id: T-SDLC-8-20.5
  authored-stage: merge
  deps: [T-SDLC-8-20.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-20.6 [Verify landed T8.20 — Managed: provision private PostgreSQL with restore settings]
  Stage: verify-landed
  canonical-id: T-SDLC-8-20.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-20.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-21.1 [Preflight T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: preflight
  canonical-id: T-SDLC-8-21.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-19.6, T-SDLC-8-8.6, T1.4, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.19, T8.8, T1.4 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-21.2 [Implement T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: implement
  canonical-id: T-SDLC-8-21.2
  authored-stage: implement
  deps: [T-SDLC-8-21.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Cloudflare DNS and valid origin TLS are both required for managed profile readiness. No direct public task route bypasses the ALB ingress policy. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-21.3 [Verify changed behavior and required checks T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: verify
  canonical-id: T-SDLC-8-21.3
  authored-stage: verify
  deps: [T-SDLC-8-21.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-21.4 [Independently review T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: review
  canonical-id: T-SDLC-8-21.4
  authored-stage: review
  deps: [T-SDLC-8-21.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.21 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-21.5 [Rebase merge T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: merge
  canonical-id: T-SDLC-8-21.5
  authored-stage: merge
  deps: [T-SDLC-8-21.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-21.6 [Verify landed T8.21 — Managed: provision HTTPS ALB, ACM and Cloudflare DNS]
  Stage: verify-landed
  canonical-id: T-SDLC-8-21.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-21.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-22.1 [Preflight T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: preflight
  canonical-id: T-SDLC-8-22.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-3.6, T-SDLC-8-19.6, T-SDLC-8-20.6, T-SDLC-8-21.6, T-SDLC-9-5.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.3, T8.19, T8.20, T8.21, T9.5 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-22.2 [Implement T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: implement
  canonical-id: T-SDLC-8-22.2
  authored-stage: implement
  deps: [T-SDLC-8-22.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Managed profile satisfies the same CLI plan/apply/status envelope as small-vm. An unhealthy rollout returns actual active/pending deployment state and nonzero outcome. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-22.3 [Verify changed behavior and required checks T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: verify
  canonical-id: T-SDLC-8-22.3
  authored-stage: verify
  deps: [T-SDLC-8-22.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-22.4 [Independently review T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: review
  canonical-id: T-SDLC-8-22.4
  authored-stage: review
  deps: [T-SDLC-8-22.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.22 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-22.5 [Rebase merge T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: merge
  canonical-id: T-SDLC-8-22.5
  authored-stage: merge
  deps: [T-SDLC-8-22.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-22.6 [Verify landed T8.22 — Managed: compose Pulumi program and immutable rollout adapter]
  Stage: verify-landed
  canonical-id: T-SDLC-8-22.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-22.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-23.1 [Preflight T8.23 — Select and persist per-installation deployment profiles]
  Stage: preflight
  canonical-id: T-SDLC-8-23.1
  authored-stage: preflight
  deps: [T-PROD.1, T7.1, T8.1, T-SDLC-8-22.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T7.1, T8.1, T8.22 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-23.2 [Implement T8.23 — Select and persist per-installation deployment profiles]
  Stage: implement
  canonical-id: T-SDLC-8-23.2
  authored-stage: implement
  deps: [T-SDLC-8-23.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Initializer and plan output display selected profile, resource cost assumptions and qualification state. Ordinary upgrade/apply refuses to switch an existing environment profile. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-23.3 [Verify changed behavior and required checks T8.23 — Select and persist per-installation deployment profiles]
  Stage: verify
  canonical-id: T-SDLC-8-23.3
  authored-stage: verify
  deps: [T-SDLC-8-23.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-23.4 [Independently review T8.23 — Select and persist per-installation deployment profiles]
  Stage: review
  canonical-id: T-SDLC-8-23.4
  authored-stage: review
  deps: [T-SDLC-8-23.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.23 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-23.5 [Rebase merge T8.23 — Select and persist per-installation deployment profiles]
  Stage: merge
  canonical-id: T-SDLC-8-23.5
  authored-stage: merge
  deps: [T-SDLC-8-23.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-23.6 [Verify landed T8.23 — Select and persist per-installation deployment profiles]
  Stage: verify-landed
  canonical-id: T-SDLC-8-23.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-23.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-24.1 [Preflight T8.24 — Plan explicit profile-to-profile data migration]
  Stage: preflight
  canonical-id: T-SDLC-8-24.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-23.6, T-SDLC-8-26.6, T-SDLC-10-11.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.23, T8.26, T10.11 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-24.2 [Implement T8.24 — Plan explicit profile-to-profile data migration]
  Stage: implement
  canonical-id: T-SDLC-8-24.2
  authored-stage: implement
  deps: [T-SDLC-8-24.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Both managed-to-small-vm and small-vm-to-managed have defined compatibility checks. Changing profile alone cannot execute migration or destroy source data. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-24.3 [Verify changed behavior and required checks T8.24 — Plan explicit profile-to-profile data migration]
  Stage: verify
  canonical-id: T-SDLC-8-24.3
  authored-stage: verify
  deps: [T-SDLC-8-24.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-24.4 [Independently review T8.24 — Plan explicit profile-to-profile data migration]
  Stage: review
  canonical-id: T-SDLC-8-24.4
  authored-stage: review
  deps: [T-SDLC-8-24.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.24 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-24.5 [Rebase merge T8.24 — Plan explicit profile-to-profile data migration]
  Stage: merge
  canonical-id: T-SDLC-8-24.5
  authored-stage: merge
  deps: [T-SDLC-8-24.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-24.6 [Verify landed T8.24 — Plan explicit profile-to-profile data migration]
  Stage: verify-landed
  canonical-id: T-SDLC-8-24.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-24.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-25.1 [Preflight T8.25 — Execute and reconcile an approved profile migration]
  Stage: preflight
  canonical-id: T-SDLC-8-25.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-24.6, T-SDLC-10-11.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.24, T10.11 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-25.2 [Implement T8.25 — Execute and reconcile an approved profile migration]
  Stage: implement
  canonical-id: T-SDLC-8-25.2
  authored-stage: implement
  deps: [T-SDLC-8-25.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Interruption never labels both environments authoritative for writes. Source teardown is not an implicit consequence of successful target startup. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-25.3 [Verify changed behavior and required checks T8.25 — Execute and reconcile an approved profile migration]
  Stage: verify
  canonical-id: T-SDLC-8-25.3
  authored-stage: verify
  deps: [T-SDLC-8-25.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-25.4 [Independently review T8.25 — Execute and reconcile an approved profile migration]
  Stage: review
  canonical-id: T-SDLC-8-25.4
  authored-stage: review
  deps: [T-SDLC-8-25.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.25 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-25.5 [Rebase merge T8.25 — Execute and reconcile an approved profile migration]
  Stage: merge
  canonical-id: T-SDLC-8-25.5
  authored-stage: merge
  deps: [T-SDLC-8-25.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-25.6 [Verify landed T8.25 — Execute and reconcile an approved profile migration]
  Stage: verify-landed
  canonical-id: T-SDLC-8-25.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-25.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-26.1 [Preflight T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: preflight
  canonical-id: T-SDLC-8-26.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-12.6, T-SDLC-8-13.6, T-SDLC-8-14.6, T-SDLC-8-17.6, T-SDLC-8-23.6, T-SDLC-10-10.6, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.12, T8.13, T8.14, T8.17, T8.23, T10.10 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-26.2 [Implement T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: implement
  canonical-id: T-SDLC-8-26.2
  authored-stage: implement
  deps: [T-SDLC-8-26.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Both profiles pass the common CLI contract while exposing distinct operational limits. Small-VM readiness cannot be asserted from managed RDS/Fargate evidence. Named scoped checks pass and the specified negative case is demonstrated; live/production qualification remains a separate explicitly authorized E16 gate where applicable. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-26.3 [Verify changed behavior and required checks T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: verify
  canonical-id: T-SDLC-8-26.3
  authored-stage: verify
  deps: [T-SDLC-8-26.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-26.4 [Independently review T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: review
  canonical-id: T-SDLC-8-26.4
  authored-stage: review
  deps: [T-SDLC-8-26.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.26 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-26.5 [Rebase merge T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: merge
  canonical-id: T-SDLC-8-26.5
  authored-stage: merge
  deps: [T-SDLC-8-26.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-26.6 [Verify landed T8.26 — Qualify small-VM profile and both-profile contract parity]
  Stage: verify-landed
  canonical-id: T-SDLC-8-26.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-26.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-27.1 [Preflight T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: preflight
  canonical-id: T-SDLC-8-27.1
  authored-stage: preflight
  deps: [T-PROD.1, T8.2, T-SDLC-8-8.6, T1.11, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.2, T8.8, T1.11 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-27.2 [Implement T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: implement
  canonical-id: T-SDLC-8-27.2
  authored-stage: implement
  deps: [T-SDLC-8-27.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Sender identity, DNS records and runtime send scope are explicit without exposing token values. No existing domain-wide mail policy is overwritten and no real message is sent by default tests. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-27.3 [Verify changed behavior and required checks T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: verify
  canonical-id: T-SDLC-8-27.3
  authored-stage: verify
  deps: [T-SDLC-8-27.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]

- [ ] T-SDLC-8-27.4 [Independently review T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: review
  canonical-id: T-SDLC-8-27.4
  authored-stage: review
  deps: [T-SDLC-8-27.3]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Independent reviewer is named for T8.27 with PR/base/head SHA, covered paths, checks, stable findings and dispositions; no unresolved blocking findings remain. Accepted findings append stable fix, affected verification and re-review tasks before this gate can pass.]

- [ ] T-SDLC-8-27.5 [Rebase merge T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: merge
  canonical-id: T-SDLC-8-27.5
  authored-stage: merge
  deps: [T-SDLC-8-27.4]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [GitHub REBASE merge lands the exact independently reviewed candidate after required CI and branch checks; PR URL, reviewed head and resulting main SHA are recorded; changed head/base invalidates affected verification and review.]

- [ ] T-SDLC-8-27.6 [Verify landed T8.27 — Provision transactional email identity and scoped delivery permissions]
  Stage: verify-landed
  canonical-id: T-SDLC-8-27.6
  authored-stage: verify-landed
  deps: [T-SDLC-8-27.5]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Fetched remote main contains the reviewed change; reviewed-versus-landed content and affected checks pass on the landed SHA; coordinator records only the source task acceptance actually established in execution-state.json, with component/provider/production boundaries explicit.]

- [ ] T-SDLC-8-28.1 [Preflight T8.28 — Qualify email sandbox, delivery and sender readiness]
  Stage: preflight
  canonical-id: T-SDLC-8-28.1
  authored-stage: preflight
  deps: [T-PROD.1, T-SDLC-8-27.6, T1.11, T1.10, T-PROD.15]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Current task contract, owned paths, source changes and T8.27, T1.11, T1.10 are reconciled; acceptance and required services are executable; task claim and isolated SSD worktree are assigned; shared-file conflicts and external gates are resolved before implementation.]

- [ ] T-SDLC-8-28.2 [Implement T8.28 — Qualify email sandbox, delivery and sender readiness]
  Stage: implement
  canonical-id: T-SDLC-8-28.2
  authored-stage: implement
  deps: [T-SDLC-8-28.1]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [Identity qualification can depend on this gate without depending on combined E16, avoiding a release dependency cycle. Qualification report distinguishes mocked checks, sandbox send, actual authorized receipt and production-account readiness. Candidate source/head, owned diff, test changes and public-safe handoff are recorded; this stage does not accept the product task.]

- [ ] T-SDLC-8-28.3 [Verify changed behavior and required checks T8.28 — Qualify email sandbox, delivery and sender readiness]
  Stage: verify
  canonical-id: T-SDLC-8-28.3
  authored-stage: verify
  deps: [T-SDLC-8-28.2]
  status-source: docs/planning/sdlc-stage-state.json
  reported-status: UNRECORDED
  authority: reported-display-only
  Acceptance: [At the exact candidate head, task-contract verification and negative checks pass with required real services; changed API/UI behavior has actual boundary/browser evidence where applicable; relevant format, lint, security, schema and required CI checks pass; missing services fail visibly and fixtures do not qualify providers.]
