# Continuity transaction repository

The repository implements the adopted application persistence contract. All
queries select installation, application, environment and workspace together.
These IDs and actor IDs are selectors; callers must establish current authority.
There is no HTTP, sending, deletion, seeding or installation migration API.

The caller owns a READ COMMITTED transaction and serializes repository calls.
**Roll back on every repository error**, including validation errors. A mutation
can have updated a row before its activity insert fails. Results are provisional:
only a successful caller commit permits reporting success or disclosing them.
The repository never commits, rolls back or retries. Database diagnostics are
replaced by fixed sentinels and failures return zero outputs.

`NewWithDBTX` accepts the canonical `operation.DBTX` interface for a future
trusted operation callback. Every query uses that exact interface; the repository
does not unwrap it or extend its lifetime. Nil interfaces, typed nils and invalid
scopes fail before I/O. Construction does not prove current authority or a guarded
callback. The legacy `New(*sql.Tx, Scope)` path retains its caller-owned transaction
behavior. Actual guarded callback integration remains a separate qualification gate.

The unallocated fragment at `../migrations/continuity.sql` is not registered or
applied automatically. An integrator must review immutable migration history
before any installation use. Runtime tests require an operator-created fresh
schema containing that exact fragment and a runtime-only TLS profile in
`AMOS_CONTINUITY_RUNTIME_TEST_CONFIG`. Tests do not provision schema, obtain
administrator credentials or skip when the required service is absent.

Run the package tests, race tests, vet and pinned lint under the execution guard.
For offline validation use `go test -run 'TestInvalidBeforeSQL|TestPersistedValidation'`;
this does not qualify persistence. Run the entire package with the scoped fixture
for actual PostgreSQL verification. Synthetic test rows use fresh four-part
scopes. No live provider or composed host qualification follows from these tests.

`PrelockMutation(ctx, action, id)` supplies the finite resource-order prerequisite
for `case.changed`, `checklist.changed`, `procedure.changed` and `draft.saved`.
It discovers without resource locks, acquires the complete bounded closure in
UUID order with fixed kind ties, and rejects a changed root snapshot before
related-row validation. Property/source rows are SHARE-held, mutable rows UPDATE-
held. A missing draft is protected by its held case parent only under the complete
cooperating writer protocol; absence itself has no row lock.

Call it once in the same writable READ COMMITTED transaction after current
identity/workspace/billing authority and before capacity or the one declared
mutation. Roll back on any error. Its copied `LockedResource` values are
observations, never authority, a supplied lock plan or a cross-transaction grant.
Legacy methods retain their behavior and must not be treated as a qualified
mixed standalone writer graph. Full native composition, guarded callback scope,
Root journaling, final F and commit/publication remain separate gates. See the
[prelock contract](../../../docs/contracts/continuity-resource-prelock.md).
