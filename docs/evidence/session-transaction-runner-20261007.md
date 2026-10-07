# Session transaction-only constructor component

[PR32](https://github.com/ajent-social/AMOS/pull/32) landed independently reviewed
candidate `cda605902bb1bea4415a9a4f87e717d160c9dc31` against
`e42634c74630699904cb3374573eb2a3f4868f7a` as
`92bf892e68e58a6f4e16fabedf1b7c1e1d3f545f`. All three source/test files equal
reviewed bytes; original author revision `5adc1b9` remains preserved.

The additive `NewWithTxRunner` accepts the consumer-owned transaction capability,
rejects nil and typed-nil inputs without I/O, and preserves the legacy constructor
and configuration shape. No pool lifecycle or schema authority is added.

Independent normal/race unit checks, scoped vet and pinned lint passed. Actual
TLS PostgreSQL runtime-role required-service normal and race checks each passed
six entries (parent plus five subtests), with no skips or failures. Typed-nil and
actual failed-commit credential-leak mutations failed their intended assertions;
exact restoration passed. Review verified rebase byte custody and unchanged
relevant dependency context. A fresh coordinator check at the landed revision
passed the same six required-service entries.

Legacy privileged suites were not run. This local component evidence does not
qualify all authentication, production database roles, a production host, cloud
providers or deployment. Hosted CI success is not claimed.
