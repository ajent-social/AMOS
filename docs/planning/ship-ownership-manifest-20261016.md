# Ownership manifest component delivery

Requested batch ceiling: 10. Actual assignment: one coherent T9.8 component,
current-session execution with inherited model and reasoning; no overrides.
Token/cost telemetry is unavailable. This record grants no deployment authority.

| Batch | Outcome | Members | Owner | External prerequisites | Verification | Delivery gates |
|---|---|---|---|---|---|---|
| Ownership manifest | Canonical relative ownership metadata bound to actual supplied baseline bytes | T9.8 bounded component | L12 implementer; coordinator integrates | None for local library; actual generator compatibility remains open | Scoped race, vet, lint, deterministic render and rejected path/baseline/ownership mutations | Independent exact-head review, guarded rebase merge, fresh landed checks |

## Scope and acceptance

The library renders a new internal component format; it is not the initializer's
existing persisted `.amos-manifest.json` schema. Four explicit ownership classes,
immutable pin metadata and actual supplied baseline-byte verification are required.
Business paths cannot silently become generated/managed. Canonical relative paths
must reject traversal, aliases, sensitive/transient names and case collisions.
Rendering is deterministic, bounded and contains metadata rather than file contents.
No callbacks, fetches, filesystem writes or authority-bearing upgrades are exposed.

Root reserves T9.8 and integration ownership through canonical atomic claims in an
isolated worktree. Prior abandoned reservations were reconciled after the owner
confirmed no prior workers remained. All prior worktrees, branches and uncommitted
material are preserved; private recovery inventories stay outside tracked source.

## Component boundary and remaining acceptance

T9.8 remains IN_PROGRESS. The initializer already records managed/authored files;
replacing that wire format without compatibility review would lose its existing
resume/preimage guarantees. Actual generated emission, a reviewed schema migration,
durable content-addressed baseline retention/retrieval and upgrade use remain open.
Caller-supplied baseline bytes prove only local digest consistency, never trusted
provenance, persistence, release approval or permission to overwrite custom files.
No task descendant is released by this component receipt.

## Obtained evidence

Source commit `39a828321ac7fa3973d13c0b8fd6ecefb26d1dd0` implements the two
owned library files. Scoped Mac race tests and vet pass. An isolated compiled
mutation bypassing the public-path guard failed the escaping-path regression;
exact restoration followed by the full scoped race checks passes. No required
provider/database service applies to this data-only component.

The initial remote check rejected its cached image's Go1.26.9; it is not passing
verification evidence. The corrected bounded check selects exact Go1.27.1 and
will verify transferred input hashes and the pinned linter release checksum.
The corrected remote attempt completed successfully: Go1.27.1 Linux ARM64,
SHA-256 input verification for both source files, root module/sums and lint config,
checksum-verified golangci-lint2.13.2, zero issues. This is scoped remote lint,
not hosted CI. The disposable container was bounded to2CPUs/4GiB with a600second
hard command timeout; no model/provider or credential access was used.
Initial independent review and landing were pending at that verification checkpoint; see final delivery below.


## Independent review and fix obligations

Review at `0298d38f082403266badb9c47636ce801b82c003` against
`90c8ec3249562ce5e753f0fa594a3a5123ea2026` found OM-001: frozen
owner-authored `business/operations` and `business/types` roots were omitted.
The finding is accepted and blocks merge. Canonical component stages
T-OWNERSHIP-01.1 through .6 and explicit .2.F1, .3.F1 and .4.R1 retain
implementation, negative verification and independent re-review obligations.
These supplement, rather than complete, the original T9.8 stages.

All changed files pass the public-artifact scanner. The whole checkout still
reports62 findings in unchanged files; they match the existing issue135 class.
A clean whole-checkout scan is not claimed; scanner/protection policy is unchanged.


OM-001 correction commit `ca11e953338a7dc5eb661d9fb4772ff341599366` adds
both frozen authored roots. Eight new compiled cases failed before the correction,
then the full62 named scoped race checks and vet passed with zero failures/skips.
Affected exact-source remote lint now passes zero issues. The owning Wazi
validator at revision `eb90e4b25cce0c2b3d5dcd31d2207a643c6db2f5` validates the
1607-task export against frozen contract
`sha256:7582512f122d2f2a9c4461facc7541c9887053f137260d6ebe9c6dea611d039d`
with zero findings; all60 owning verifier fixtures pass. Export SHA-256:
`5ef1236e6f71a411e27779e72e605f2f56584364ce62431d86bbfe9ef69c8640`.
Transferred verifier/exporter/input hashes are retained in the private operating
receipt. This is plan conformance only, not authenticated execution/admission.
Exact-head re-review and guarded landing were pending at that checkpoint; see final delivery below.

The full-checkout scanner's62 diagnostics are byte-equal between unchanged
main and this candidate. Changed-file scans pass. No scanner exception or
branch-protection setting was added or weakened.


## Verified component landing

Independent reviewer `ownership_review` approved full candidate
`03f5ef24253bcb6c8a61bd08fecc0e40cb742d37` against
`90c8ec3249562ce5e753f0fa594a3a5123ea2026`, with OM-001 resolved and no
remaining actionable findings. [PR146](https://github.com/ajent-social/AMOS/pull/146)
landed by guarded rebase at `18643232165bff247c448bfe92da1b47f48739d9`.
Remote main reachability and whole-tree equality with the reviewed candidate
pass. Fresh landed Mac scoped race checks pass62 named cases with zero
failures/skips; current1607-task native display freshness passes.

The component delivery rows are complete; original T9.8 remains IN_PROGRESS.
No initializer emission, durable storage, upgrade or product acceptance follows.
All62 abandoned reservations were retired after owner confirmation; all122 prior
worktrees, including10 with uncommitted material, were preserved. Only fresh
owned delivery claims are eligible for release after recording this handoff.

Actual member count is1 under requested ceiling10, with one implementer and a
separate independent reviewer. Stage token/cost and exact duration telemetry
were unavailable; no throughput or acceleration claim is made. The data-only
library has no required database or provider service. No hosted CI is configured
for this candidate; only the explicitly recorded local/remote checks passed.


Closeout review correction OM-002 records the actual independent review
reservation on T-OWNERSHIP-01.4.R1 rather than the coordinator's task reservation.
The reviewer released its exact token after the approval receipt. Application
source and its verification evidence are unchanged.
