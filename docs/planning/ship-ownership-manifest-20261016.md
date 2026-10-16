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
Remote lint, independent review and landing are pending.
