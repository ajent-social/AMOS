# Pure continuity guide

This optional application-owned package implements five finite scenarios over a
caller-supplied selection. It does not add definitions or policy to the framework.
`ParseQuestion` recognizes only the five phrases in the adopted
[contract](../../../docs/contracts/continuity-guide.md), case-insensitively after
trimming, with one optional final question mark. `Answer` also accepts typed requests.

Every call validates the entire bounded selection, including unused records,
canonical UUIDv7 IDs, domain case shape, original-body SHA-256 digests and source
resolution. Missing references or cases return `ErrNotFound`, malformed values
return `ErrInvalid`, and unknown topics/questions return `ErrUnsupported`.
Failures return zero output and exact sentinels. Request validation comes first.
Successful items are non-nil and deterministically ordered; nested references are
sorted independent copies. Positive revisions through MaxInt64 can be read.

Attention and handover describe non-completed selected records. Case explanation
reports recorded status without treating it as proof of authority or action.
Spending authority remains unknown regardless of source prose. Owner updates use
`domain.PrepareDraft` and are previews only, never saved or sent; later persistence
must check the current revision separately. No source prose is interpreted as an
instruction or a new fact. Digests establish consistency, not authenticity.

Text is ordinary plain-text data, not generated markup. Validated titles can
contain markup-like characters; a later renderer must escape all values and
resolve references within authenticated scope. Source bodies allow tabs/newlines
and the domain's non-ASCII body characters, but reject other ASCII controls.

There is no I/O, clock, model, provider, permission check, recipient, external
action or mutable package state. A selection is not a complete inventory. Tests
need no service and do not qualify persistence, HTTP/UI, source authorization,
provider retrieval, deployment or succession readiness.

Package checks: `go test ./examples/continuity/guide`,
`go test -race ./examples/continuity/guide`, `go vet ./examples/continuity/guide`
and the repository-pinned lint command scoped to this package. Tests include
contract bounds, every topic/status, corrupt and missing sources, order
independence, revision extremes and deep slice isolation.
