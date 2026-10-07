# Private replay primitive component

[PR31](https://github.com/ajent-social/AMOS/pull/31) landed reviewed candidate
`ac7e229efdf433b179133ae66e36b2d24ce3bed5` against
`de3461086262d2c68a7f7bb770c8858f92a30bab` as
`e42634c74630699904cb3374573eb2a3f4868f7a`. Both new operation helper/test
files equal reviewed bytes. Original author revision d36633e remains preserved.

The private hash follows the exact frozen domain-separated byte formula. Cached
result validation checks a bound required/replay-safe definition, finite kind,
registered byte limit, SHA-256 and output codec, with safe errors and defensive
copies. It never calls a handler or establishes authority or persistence.

Author and independent reviewer each passed 17 selected top-level unit tests in
normal and race runs, zero failures/skips; scoped vet and pinned lint passed.
Independent separator, checksum and bound mutations each failed their intended
regression, followed by exact restoration and passing checks. Rebase review
confirmed unchanged source/module/lint context and exact two-file custody; a
separate fresh candidate unit run passed. Coordinator fresh landed helper tests
also passed. Changed-source public and whitespace checks passed.

Existing required-service tests were explicitly excluded for this pure component;
no SQL qualification is inferred. Stored descriptor/revision/schema identity,
current authority before disclosure, durable capacity/replay, audit/effects and
transport integration remain required. The digest is not authentication and does
not bind result kind. Full T2.8 remains IN_PROGRESS; no provider, production or
hosted CI success is claimed.
