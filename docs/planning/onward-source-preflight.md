# Source-grounded follow-on readiness

Source baseline: `c3caa036f89101cd336535a9ae2dc92f32fb6bd1`, including
independently reviewed and locally service-verified runtime storage from PR26,
private SQL adapter PR28, replay primitives PR31, session constructor PR32 and
protection constructor PR34, callback guard PR35, material constructor PR36
and transaction-only job store PR33. Additive constructor and callback guard contracts
v1.16–v1.18 landed through PR30. These components grant no operation or
production authority.

## Production host

`apphost.NewLocal` remains a development-only HTTP/loopback composition. Its
business callback receives `*storage.DB`, and it opens a separate raw SQL job
pool. A production constructor cannot safely be obtained by changing its origin
flag or reusing its migration-capable credential shape.

The frozen [constructor contract](../contracts/transaction-services.md) defines
additive transaction-only admission for session, email, login, recovery, MFA,
protection and protected mail material services. Session and protection have
landed with independent actual-service and fresh landed checks. Material has also landed with independent actual-service and fresh landed
checks. Email and MFA have landed with independent actual-service and fresh landed
checks. Login and recovery remain unimplemented for this seam. Typed-nil handling and legacy compatibility remain mandatory.
The [job-store contract](../contracts/transaction-job-store.md) defines an additive
transaction-only adapter without exposing the runtime pool or another DSN. Jobs source and its corrected post-lock regression have landed after different
independent review, actual normal/race and fresh landed service checks.

Production composition needs an explicit canonical HTTPS origin, secure cookie
and trusted proxy policy, finite route/protocol manifest, runtime-only credential,
real mail capability and key references, read-only schema readiness and bounded
lifecycle. Existing secure session-cookie support does not qualify the composition.
Actual composed PostgreSQL/session/business-route tests remain required. Production
role grants, network, provider, budget and DNS decisions remain external gates.

## Operation executor

The immutable registry is landed, but raw handlers and registry binding confer no
operation authority. No authority-granting executor is dependency-ready.

The adopted prerequisite is a private caller-transaction adapter in new
`app/operation/sqltx.go` and `sqltx_test.go`. `*sql.Tx` does not implement existing
`operation.DBTX`: its row result types differ from the interface return types.
A private non-embedded adapter could delegate exactly one existing transaction,
return a nil Rows interface on query error, and expose no commit, rollback, pool,
retry or invocation entry point. The coordinator has adopted this two-file private-helper assignment in the
integration assignment record. The private component now landed through PR28
with independent actual PostgreSQL normal/race and landed checks; see
[component receipt](../evidence/operation-sql-adapter-20261007.md). It does not
complete the executor or qualify a provider.

Full dispatch still requires reviewed current-session/authority and policy
adapters, lock ordering compatible with writers, replay/capacity storage and
allocated migration, finite invocation audit, scoped typed effects, trusted
strict codecs/generator binding and composed transport tests. Stale billing
positives or financial-boundary crossings must preserve the operation contract's
Unavailable semantics rather than inherit a catalog denial without reconciliation.
Exact replay must recheck current authority before disclosing cached results.
These shared contracts and database gates precede executable dispatch.

The private adapter is not the frozen callback wrapper. The separate v1.18
[callback contract](../contracts/callback-database.md) requires rejection of direct
transaction-control statements and invalidation of retained DBTX, Row and Rows
after callback return. Guard source has landed after a separate cancellation-test correction, different
independent actual-service review and fresh landed checks. Handler-before-codec integration is a further
explicit gate. Registry binding and these helpers confer no operation authority.

## Next current-session authority seam

A concrete [session recheck proposal](session-recheck-preflight.md) records the
missing opaque middleware provenance and post-lock database-time checks. It is
not frozen or implemented. Rotation and other writer-order incompatibilities remain
explicit dispatch blockers; transaction-only constructors do not resolve them.
