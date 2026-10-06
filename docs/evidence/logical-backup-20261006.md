# Consistent logical backup source evidence

PR [15](https://github.com/ajent-social/AMOS/pull/15) landed at
`cbceb0413ce48bb17e7eebe4ffe2980cff4c1cd2`. Reviewed head was
`687b6d71e3f8af4869e76853fc4fa2e75372397e`, on reviewed base
`1163655a86399943029464dc2b6da25a596ca1aa`. Both owned source files match
landed main byte for byte. GitHub reported a clean merge with no required check
entries; this records local verification, not hosted CI success.

The adapter holds an exported repeatable-read snapshot while reading the
migration ledger and running the pinned compatible pg_dump. The manifest binds
scope, versions, consistency point, start/finish, size and archive digest.
Required byte/time limits and private connection files bound the operation.
A no-overwrite completion marker is written only after successful archive
finalization and credential removal. A canceled or failed dump has no selectable
completion artifact. Filesystem sync is not an interruptible hard deadline;
subprocess waits and database cleanup have explicit finite bounds.

## Verification and review

- Author `logging_verify` implemented the two owned files. Independent reviewer
  `bootstrap_fix` found timeout cleanup and database-failure coverage gaps.
- Review fixes added bounded cleanup, a pre-publication cancellation check,
  subprocess inherited-pipe waiting and real missing-database coverage. The root
  coordinator independently reviewed the final fixes and exact source head.
- PostgreSQL 16.15 and pg_dump/pg_restore 16.14 supplied real required-service
  checks. `go test -race ./internal/backup -count=1`, scoped `go vet` and
  `golangci-lint` passed. Root repeated the creation/cancellation/pipe cases.
  Each command respected the shared machine load gate.
- The midstream case waited for actual partial archive bytes before cancellation;
  it verified the incomplete backup directory was absent. A missing database
  failed without an artifact, and an existing destination remained unchanged.
- Removing the completion context guard made
  `TestCompletionManifestCancellationDoesNotPublish` fail; restoring the exact
  guard passed. The test was run in the isolated candidate only.
- An initial root run lacked an exported database variable and failed visibly.
  Exporting the owned fixture configuration produced the passing run.
- The public-artifact scanner returned nine credential-assignment matches.
  Exact-source inspection cleared them as dynamic Go expressions/bindings,
  equality comparison, synthetic loopback fixtures and format-string text.
  No actual credential or private locator was found; scanner success is not claimed.

## Acceptance boundary

T10.7 is accepted as the local logical-backup source adapter. Listing a custom
archive with pg_restore is format validation, not a restored-application test.
Encrypted remote retention, complete restore execution, scheduler freshness/RPO,
managed-service privileges and production qualification remain separate tasks.
Managed automated backups/PITR complement this logical backup; crash-consistent
VM snapshots do not replace a verified logical backup.
