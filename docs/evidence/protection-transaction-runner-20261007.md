# Protection transaction-only constructor component

[PR34](https://github.com/ajent-social/AMOS/pull/34) landed independently reviewed
candidate `85cce23a99691e928ce152b2af7dd30ca030cac8` as
`f00fdff60cd94b9fd181e54ff890e63ae6e02dcc`. Its reviewed base was
`e42634c74630699904cb3374573eb2a3f4868f7a`; immediate live main was
`92bf892e68e58a6f4e16fabedf1b7c1e1d3f545f`, adding only the separate session
constructor slice. The exact reviewed PR head was retained, and all four landed
source/test files equal reviewed bytes. Original author revision `32f69f6` remains
preserved.

`NewWithTxRunner` and the database-free `TxConfig` preserve legacy construction,
key snapshots and existing admission/prune SQL. The private dependency exposes
transaction execution without pool lifecycle or schema authority.

Independent selected normal/race checks each passed 26 entries, with zero skips
or failures. Actual TLS PostgreSQL runtime-role normal/race checks each passed
three entries (parent plus two subtests). Key-copy and seven typed-nil mutations
failed intended assertions; exact restored source passed. Scoped vet and pinned
lint passed. A fresh coordinator check at the landed revision passed the same
three required-service entries, and owned test cleanup passed.

Legacy privileged suites were not run. This supplemental local component does
not qualify all authentication, production roles or hosts, cloud providers or
deployment. Hosted CI success is not claimed.
