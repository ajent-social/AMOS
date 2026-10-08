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
