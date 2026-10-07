# Source-grounded follow-on readiness

Inspected source: `64ba2c8494fdef39da72a53df2bfcb495f79ec1d`, with separately
reviewed runtime-storage candidate `79d3014c85c1a5884927bcff05f3e664b4f101a9`.
These are preparation findings, not adopted APIs, source acceptance or dispatch
permission. Current storage service gates take priority.

## Production host

`apphost.NewLocal` remains a development-only HTTP/loopback composition. Its
business callback receives `*storage.DB`, and it opens a separate raw SQL job
pool. A production constructor cannot safely be obtained by changing its origin
flag or reusing its migration-capable credential shape.

A later reviewed assignment must define transaction-only constructor admission
for session, email, login, recovery, MFA, protection and protected mail material
services. Those current implementations consume WithTx but require `*storage.DB`.
Typed-nil handling and compatibility tests must accompany any interface change.
`jobs/sqlstore.New` separately requires `*sql.DB`; a transaction-based worker
adapter must be defined without exposing RuntimeDB's pool or opening another DSN.

Production composition needs an explicit canonical HTTPS origin, secure cookie
and trusted proxy policy, finite route/protocol manifest, runtime-only credential,
real mail capability and key references, read-only schema readiness and bounded
lifecycle. Existing secure session-cookie support does not qualify the composition.
Actual composed PostgreSQL/session/business-route tests remain required. Production
role grants, network, provider, budget and DNS decisions remain external gates.

## Operation executor

The immutable registry is landed, but raw handlers and registry binding confer no
operation authority. No authority-granting executor is dependency-ready.

The smallest proposed prerequisite is a private caller-transaction adapter in new
`app/operation/sqltx.go` and `sqltx_test.go`. `*sql.Tx` does not implement existing
`operation.DBTX`: its row result types differ from the interface return types.
A private non-embedded adapter could delegate exactly one existing transaction,
return a nil Rows interface on query error, and expose no commit, rollback, pool,
retry or invocation entry point. This proposal is not yet adopted or implemented.
Real PostgreSQL transaction visibility, rollback, cancellation and lifecycle
checks are required before its acceptance; mocks are insufficient.

Full dispatch still requires reviewed current-session/authority and policy
adapters, lock ordering compatible with writers, replay/capacity storage and
allocated migration, finite invocation audit, scoped typed effects, trusted
strict codecs/generator binding and composed transport tests. Stale billing
positives or financial-boundary crossings must preserve the operation contract's
Unavailable semantics rather than inherit a catalog denial without reconciliation.
Exact replay must recheck current authority before disclosing cached results.
These shared contracts and database gates precede executable dispatch.
