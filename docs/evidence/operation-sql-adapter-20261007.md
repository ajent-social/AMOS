# Private SQL transaction adapter component

[PR28](https://github.com/ajent-social/AMOS/pull/28) landed as
`cafb6d638bca4f226db8a5e764788c6b7a516c79`, from independently reviewed
`178ce7424a8dabe77636772ab184d308b5229117` against
`f60759556914df571a32193a6cc21af445913bd1`. Both new files,
`app/operation/sqltx.go` and `sqltx_test.go`, equal reviewed bytes on main.

The private adapter reconciles sql.Tx result types with DBTX while preserving
caller transaction ownership, contexts, arguments, results and errors. Query
failure returns a genuinely nil Rows interface. It adds no exported constructor,
pool/lifecycle accessor, operation entry point or production call site.

Scoped unit tests, vet and pinned lint passed. The initial independent reviewer
ran actual PostgreSQL normal/race checks and found that commit/rollback tests
incorrectly supplied an explicit value for an identity column. A different author
changed the test to INSERT(value) RETURNING id, retaining adapter UPDATE and
RowsAffected coverage and cleanup of only successfully allocated identities.
A different final reviewer cleared the full exact source and independently passed
all 61 package test entries normally and with race detection, including all five
required-service entries, with zero skips/failures. Transaction visibility,
commit/rollback, cancellation, SQL errors and completion behavior were exercised
against actual local TLS PostgreSQL. Prior typed-nil negative/restored evidence
and the genuine pre-fix service failures remain recorded; absent service fails
visibly rather than skipping.

After immediate head/base/check readback and guarded rebase merge, the coordinator
verified both files and repeated all five required-service entries at the landed
revision: five pass, zero skip/fail. Local checks are not hosted CI success.
The fixture operator subsequently verified exact-owned resource and temporary
credential cleanup; no service is left running for this batch.
Publication scanning matched an empty-password validation expression, manually
inspected as source rather than a credential; scanner success is not asserted.

This is a private result adapter, not the frozen callback wrapper. Before callback
exposure, a separate guard must reject direct transaction-control statements and
invalidate retained DBTX, Row and Rows values after callback return. Current
session/authority, lock ordering, replay/capacity, audit/effect, migration and
transport gates remain open. Full T2.8 stays IN_PROGRESS; no provider or production
qualification follows from this component.
