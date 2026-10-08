# Continuity repository source component

PR53 adopted the storage-only contract at
`dfc4e458a98c622b45b2e6dc83a1e80a8e7bc8a4`, independently reviewed at
`c7a12c3140c5376f5d308a4a781bdddaeca02caa`. All five reviewed files match.
Fresh landed semantic validation and the complete 1,586-task export passed the
pinned Wazi validator with zero findings and `authorityAuthenticated=false`.
All original 1,501 records and existing added IDs remain. The full host contract
and original product acceptance gates are not completed by this design.

PR57 landed the repository at `09e932849993ece6e0f3d3a1eba4ddc99dd1bf2a`,
from independently reviewed `c61be8554b57026ba3cab97df0cfa58c3cf5568b`.
All nine source, test, README and SQL files match the reviewed blobs.

The application repository implements scoped register/source discovery, literal
search, typed values and source hashes, revision-checked maintenance/checklist/
procedure changes, saved unsent drafts and atomic activity. It accepts an explicit
caller-owned transaction; scope and actor IDs are selectors, never authority.
The caller must roll back on every error and wait for commit before disclosure.

## Verification obtained

The application SQL fragment was independently inspected at schema commit
`ebded2813730c635a3985cb8a75ab55e9ff69ba4`, SHA-256
`f00ab44126762289ae91f422f59bc6a6e5700dcfc3f3be8a498195d3b6e0d2c3`.
A separately reviewed bounded setup applied it to a new isolated local database.
The actual runtime role had finite application-table DML privileges; tests used
no schema creation or administrator credentials. No existing installation changed.

The author and a different reviewer each ran:

- `go test -count=1 -json -timeout=60s ./examples/continuity/repository`
- `go test -race -count=1 -json -timeout=60s ./examples/continuity/repository`
- `go vet ./examples/continuity/repository`
- Pinned `golangci-lint` 2.13.2 on the package.

Each full normal/race package run had 39 pass events and zero skips. Vet and lint
passed. These totals include unit checks as well as actual PostgreSQL cases;
they are not counts of live providers or separate deployed journeys. The author
also confirmed that a missing required fixture fails visibly.

Real service checks include each scope dimension, composite foreign keys,
source digest corruption, strict checklist wire shape, bounded JSON storage,
keyset pagination and literal search, revision contention, human-decision denial,
explicit repeated draft save, atomic activity, caller rollback and failed activity
insertion under a real conflicting table lock. A preliminary review added finite
JSON-size constraints/defensive read projections and bounded actual query contexts
before the final source head was frozen.

The author removed case activity in a disposable overlay and observed the
intended activity assertion fail. The reviewer independently regenerated that
mutation and a source-hash bypass, observing their intended actual-service
assertions. Original files remained unchanged; the restored full package passed
39 events. The fresh landed full package also passed 39 events with zero skips.

## Limits

The SQL remains an unallocated application fragment. No installation migration,
HTTP/SSR host, current authority, production role, provider, deployment or complete
replacement rehearsal is qualified. The twelve storage-contract/data lifecycle
rows record these bounded reviewed and landed stages only. Original product
acceptance and the full scope remain unchanged.

The focused scanner reports one runtime configuration-field forwarding heuristic
in the test. Inspection found no credential literal; no suppression or full-green
public scan is asserted. Hosted CI was not used as this delivery gate.
