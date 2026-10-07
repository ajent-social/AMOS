# Transaction-only job-store component

[PR33](https://github.com/ajent-social/AMOS/pull/33) landed reviewed candidate
`8340b0a1ab72399241a099389ba5761ef0358d82` as
`c3caa036f89101cd336535a9ae2dc92f32fb6bd1`. Reviewed base was
`f00fdff60cd94b9fd181e54ff890e63ae6e02dcc`; immediate live base was
`bff204f8bdad5fe29e6dceeb73f06649521f9487`. Intervening callback/material
changes did not alter jobs or module files. All four landed files equal reviewed
bytes, and original source/correction commits remain preserved.

The additive `NewWithTx` retains legacy construction and caller-owned enqueue
behavior. Standalone operations use one transaction, expose results only after
success, and resolve leases using database time after acquiring the row lock.

The first independent old-behavior negative failed lock observation, so it was
not counted as qualification. A separate author changed only the regression test
to observe the exact resolver backend. A different final reviewer reproduced the
intended stale-success assertion failure with the original update behavior;
exact restoration passed. Required TLS PostgreSQL normal/race each passed ten
entries (nine subtests and parent), with zero skips/failures. Independent selected
unit normal/race, provisional-result negative/restored, scoped vet and pinned lint
passed. Fresh landed required-service verification passed the same ten entries.

The final landed tree exactly equals the independently checked combined source
tree: root-module build and 60 selected combined unit entries passed. All 12
combined changed files match candidates; module/lock/migration files were
unchanged. Two public scanner findings were source expressions, not credential
literals; no automatic full-scan pass is claimed. Legacy privileged suites,
provider behavior, production hosts and hosted CI success are not qualified.
