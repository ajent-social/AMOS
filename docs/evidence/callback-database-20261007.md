# Private callback database guard component

[PR35](https://github.com/ajent-social/AMOS/pull/35) landed independently reviewed
candidate `acf0fd1ff6bc337d7e6f74bfaa459ddd992d22c1` against
`f00fdff60cd94b9fd181e54ff890e63ae6e02dcc` as
`6cbc513cba11e08f3fb84d34c87a5d09f21ef3a7`. All four source/test files match
reviewed bytes; original source and correction commits remain preserved.

The private factory enforces finite SQL admission, a single outstanding
operation/result lease, and invalidation of retained database/row handles. Inner
Scanner wrappers shield database/sql locks from caller panics before cleanup and
rethrow the original panic. No operation handler or executor is wired here.

Different final review cleared the complete four-file source. Selected unit
normal/race checks each passed 325 entries with no skips or failures; scoped vet,
pinned lint and bounded fuzz passed. Actual TLS PostgreSQL required normal/race
checks each passed 13 entries (12 subtests and parent), including actual deadlock
SQLSTATE 40P01 and both Scanner panic branches.

The first service review found an overly strict nil-only cancellation rollback
assertion. A separate author changed only the test: it now requires backend
termination after any rollback error and absence of surviving transaction writes.
Final actual checks observed the pinned driver's connection-lock error and proved
both server outcomes. An independent mutation committed the probe write outside
the transaction; the surviving-write assertion failed as intended, then exact
restoration passed. This mutation proves write-survival detection, not a separate
negative for the backend-disappearance check. No cleanup errors were emitted.

Fresh landed required-service verification passed all 13 entries, with no skips
or failures. The final combined landed tree equals the independently built and
unit-checked aggregate. Handler-return invalidation before codec work, current authority,
replay/capacity persistence, audit/effects and full executor integration remain
open. T2.8 is not accepted. No provider, production or hosted CI success is claimed.
