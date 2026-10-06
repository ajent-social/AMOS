# Immutable operation registry source evidence

PR [20](https://github.com/ajent-social/AMOS/pull/20) landed at
`8181fc48bf12cce569ab50d68cd261cca0ebd071`. The independently reviewed head was
`0a098c60210252bb3829cdda8753f88d9644d280`, against base
`229885fa50ee52237ebe9c3d3228b1c3b30879e3`. All three changed source files match
landed main byte for byte. Hosted checks were empty; this is local evidence.

## Delivered boundary

`policy/contracts.go` defines the frozen three-state decision and requirements
interfaces without an evaluator or default-allow implementation. The two
`app/operation` files implement typed binding and an immutable registry: complete
metadata validation, schema-digest agreement, typed-nil rejection, route conflicts
and reservations, canonical descriptor hashing, and defensive metadata copies.
Explicit empty collections remain distinct from missing collections. No exported
method invokes a handler, decoder, resolver or executor.

Codec digest equality checks binding consistency between trusted immutable
implementations. It does not prove the codec validates its declared schema. The
v1.13 design came from PR18; v1.14 finite host routing from PR19 is a separate
implementation assignment and is not qualified by these registry tests.

## Verification

- Author `logging_verify` ran `go test ./app/operation -count=1`,
  `go test -race ./app/operation -count=1`, `go vet ./app/operation`, and
  `golangci-lint run ./app/operation` successfully at the reviewed source head.
- Independent reviewer `bootstrap_fix` cleared that head and repeated
  `go test -p=1 ./app/operation -count=1`. Removing codec/metadata digest
  comparison made `TestBindRejectsSchemaDigestMismatch` fail as expected;
  restoring the exact source bytes made the regression pass.
- The root coordinator verified the frozen policy file matches its previously
  independently reviewed bytes, repeated changed-path public/whitespace checks,
  compared every landed source file, and ran the package test on landed main:
  PASS. Go commands obeyed the load gate and used isolated external caches.
- Tests cover metadata immutability, every digest-bound field, canonical set
  ordering, one-time codec digest sampling, zero/mismatch rejection, explicit
  empty collections, typed nil, route conflicts and concurrent read-only lookup.

## Remaining T2.8 work

T2.8 remains IN_PROGRESS. No evaluator, current-authority/session adapter,
transaction wrapper, invocation SQL store, replay/capacity persistence, audit or
effect validation is implemented by this slice. The generator must bind trusted
revision, schema artifacts, output privacy and replay bounds into complete
metadata; the existing business descriptor JSON does not imply those values.
Full decoding/schema behavior, real PostgreSQL atomicity/revocation/replay tests,
transport integration and executor dispatch remain gated. No provider, production
host, deployment or complete-product acceptance is claimed.
