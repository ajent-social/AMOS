# Protected material transaction-only constructor

[PR36](https://github.com/ajent-social/AMOS/pull/36) landed independently reviewed
candidate `4a2273dd5c4f2f1b880891a65eda8a444e19948c` as
`bff204f8bdad5fe29e6dceeb73f06649521f9487`. Reviewed base was
`f00fdff60cd94b9fd181e54ff890e63ae6e02dcc`; immediate live base was
`6cbc513cba11e08f3fb84d34c87a5d09f21ef3a7`, adding separate callback files.
All four landed source/test files equal reviewed bytes; original commits remain
preserved.

The additive constructor accepts only the transaction capability, retains the
legacy configuration and encrypted material semantics, and snapshots key input.
A separate compatibility fix makes the legacy test close its configured database.
The coordinator corrected the strict runtime-test decoder to admit the standard
eleven-field fixture format. A different reviewer cleared that exact correction;
the old six-field decoder failed the intended regression, and exact restoration
passed. Unknown fields, invalid types and trailing JSON remain rejected.

Independent selected normal/race each passed 17 entries; scoped vet and pinned
lint passed. Earlier independent nil/key-alias negative/restored evidence was
preserved. Actual dedicated TLS PostgreSQL required normal/race each passed one
top-level test, covering all three purpose writers, caller commit/rollback and
cancellation, scope isolation, encryption, rotation, tampering, expiry, pruning
and exact UUID cleanup. Fresh landed verification passed that required test.

The combined landed source tree equals the independently built and unit-checked
aggregate. The changed-path scanner reports one source-expression match
(`Password: cfg.Password`), not a credential literal; no automated full-scan pass
is claimed. Legacy privileged suites, mail delivery, production key/role policy,
providers, deployment and hosted CI success remain unqualified.
