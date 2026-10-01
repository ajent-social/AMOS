# Generated CI trust contract

Status: design contract for generated fragments. Root CI, required-check
configuration, release authority, and deployment wiring remain integrator-owned.
This document and its local contract tests do not create or qualify workflows.

## Ownership and workflow layout

The repository root owns workflow entrypoints and their trigger declarations:

- `.github/workflows/ci.yml` owns validation and required-check reporting.
- `.github/workflows/release.yml` owns trusted artifact and signing orchestration.
- `.github/workflows/deploy.yml` owns deployment orchestration and protected
  environment selection.

These are ownership names and intended responsibilities, not files asserted to
exist. Root workflow changes require the integrator's separate task. The
scaffold lane owns generated job fragments under
`internal/scaffold/ci/fragments/`; generated project copies use
`.github/workflows/amos-*.yml` and remain subject to a managed ownership and
conflict check. A fragment cannot add root event triggers, elevate its
permissions, bypass root conditions, or rewrite protected-check policy.

## Events and trust

| Event | Validation | Artifact/signing | Deployment |
|---|---|---|---|
| `pull_request`, including forks | Run credential-free checks against the proposed merge. | Excluded. | Excluded. |
| `merge_group` | Run the same required validation checks against the merge queue candidate. | Excluded. | Excluded. |
| `push` to a protected branch | Run validation. Trusted artifact work may run only when root policy confirms the protected ref. | Allowed only for a trusted protected ref and the protected release environment. | Allowed only for a trusted protected ref and the named deployment environment. |
| `release` | Validation may rerun against the release tag. | Allowed only for a verified release tag/ref and protected release environment. | Allowed only for an approved release and protected deployment environment. |
| `workflow_dispatch` | May run validation for the selected revision. | Allowed only when root resolves a protected trusted ref and the protected release environment is approved. | Requires that trusted ref and the protected deployment environment. |

No privileged job runs for `pull_request_target`, including when a workflow
checks out or executes the pull request head. Fork status, event kind, trusted
ref, and environment are independent conditions; a job must satisfy every
condition declared for it. Pull request code, artifacts, caches, titles,
branch names, and generated files are untrusted inputs. They cannot supply
credentials, select a privileged environment, or establish a trusted ref.

`internal/scaffold/ci.CanRun` captures this job-level rule: unprivileged jobs
run only for listed, recognized event kinds. Privileged jobs are additionally
restricted to `push`, `release`, and `workflow_dispatch`; listing a pull request,
`pull_request_target`, `merge_group`, or an unknown event cannot make it a
privileged trigger. They also require a non-fork event, a root-verified trusted
ref, and an exact protected environment. Unprivileged jobs may handle
`pull_request` and `merge_group` without credentials, including fork PRs.
Root workflows must enforce equivalent conditions using platform-provided
event/ref context and protected environment controls; a fragment's own input
is not proof of trust.

## Stable checks, limits, and permissions

Required check names are stable, globally distinct contexts: `AMOS / Validate`,
`AMOS / Merge Queue`, and `AMOS / Build Artifact`. Existing branch protection
must be migrated by the integrator when a name changes; generated fragments
cannot silently create or rename required contexts.

| Job class | Timeout | Concurrency | Permissions |
|---|---:|---|---|
| Validation / merge queue | 20 minutes | Cancel superseded runs for the same PR or merge group; do not cancel another change's run. | `contents: read`; all other token permissions `none`. No secrets. |
| Artifact build / signing | 30 minutes | Serialize by trusted source revision and release environment; never share PR cache/artifact namespaces. | Start with `contents: read`; grant only the signing or attestation permission required by the selected root-owned mechanism. |
| Deployment | 45 minutes | Serialize per installation/environment and reject stale source revisions. | Start with `contents: read`; grant only the deployment permissions scoped to the protected environment. |

Validation runs without cloud, signing, deployment, or repository-write
credentials, including on fork PRs. Privileged permissions are job-scoped, not
workflow-wide, and use least privilege. No privileged job consumes artifacts,
caches, or generated outputs produced by an untrusted event as executable
input. A failure to establish a required trusted condition denies the job.

## Immutable pins and updates

Every third-party action and reusable workflow reference uses a full immutable
40-character commit SHA. Tags, branches, abbreviated hashes, and mutable image
tags are not acceptable pins. Include a short comment identifying the upstream
project and reviewed release/version beside each pin.

Pin updates are explicit reviewed changes: inspect the upstream release and
full commit, review the diff and permissions, update the pin and version
comment, run credential-free validation and trust-boundary negative tests, and
obtain integrator review before merge. Security fixes may use the same process
with expedited review; no automatic tag-following update is permitted.
