# Post-lock challenge consumption freshness

PR65 landed `6700c605f65a85c8af4c9b2cea9d2ecdf5255d65` from independently
reviewed `c4d0986db59a2ac6c73b7d92fca00f77272b96f1`, against the adopted
[v1.22 contract](../contracts/challenge-consumption-freshness.md).
The source author and reviewer were different sessions. Both landed blobs match
the reviewed source: only ConsumeChallenge's body/comment and the new required
challenge freshness test file changed.

ConsumeChallenge requires READ COMMITTED, locks the exact ID/purpose/digest row,
then samples database time separately. Strict expiry and consumed_at use that
same post-lock instant. Errors retain zero output and existing sanitized
sentinels; the caller owns commit, rollback and the provisional result.

## Obtained verification

Author and independent reviewer each used a fresh, bounded, real TLS PostgreSQL
runtime-only fixture, with exclusive serial grants. The independent checks were:

- `go test -json -count=1 -timeout=60s -run '^TestChallengeFreshness' ./identity/store`
- `go test -race -json -count=1 -timeout=60s -run '^TestChallengeFreshness' ./identity/store`
- `go vet ./identity/store`
- Pinned `golangci-lint` 2.13.2 on `./identity/store`.

Normal and race each passed 30 tests/subtests plus the package (31 pass events),
with zero skips. Vet, lint and a scoped scan of both changed files passed. Tests
observe the exact backend blocked by the held challenge row, transaction and
statement starts before the boundary, and database time crossing expiry before
release. They cover live consumption, committed replay, caller rollback,
competing consumers, purpose/digest/unknown/consumed/expired denials, invalid
input before SQL, unsupported isolation, cancellation and statement errors.

The author's original-method transaction-clock overlay and pre-lock-clock overlay
each failed the intended expired-waiter assertion: consumption returned success
instead of unavailable. The independent reviewer generated separate overlays
using transaction-start time and moving the sample before the row lock; both
failed that same assertion. Production source remained unchanged. Fresh restored
checks passed all 31 events, and a fresh landed required-service run also passed
31 events with zero skips. Exact-owned fixture cleanup completed afterward.

Exact-equality and post-lock error probes forward the actual exported method's
statements to a real transaction through the existing test-only facade. They
supplement the unmodified two-connection waiter schedules; they do not establish
naturally scheduled equality. An initial reviewer result-parser assertion read
JSON-escaped output instead of decoded Output fields; correcting that parser and
rerunning both mutations confirmed the intended failures without source changes.

## Limits

This qualifies the bounded store primitive, not complete password recovery,
magic-link or email confirmation. Email confirmation has separate inline SQL.
Person/session/challenge writer ordering, policy waits, provenance, refreshed
principal handoff, resource authority, host composition and providers remain
open. T2.8 stays in progress; the 59 accepted product tasks are unchanged.

Only the required challenge selection was exercised; legacy schema-creating
integration suites and full-repository race checks were outside this fixture
grant. Runtime credentials and production roles were not qualified. Hosted CI
was not the delivery gate. The scoped public scan does not erase pre-existing
repository findings or imply a full-green scan.
