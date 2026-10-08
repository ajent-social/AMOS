# Current-session database composition resolution

Status: proposed resolution of the v1.24 same-database gate; not adopted, not
implemented and not a host or executor contract. Source baseline:
`fa6c5a35e61ed4f866652acc66addec1d76fb368`.
Read this with [the recheck candidate](current-session-recheck.md) and
[the host contract](continuity-host.md). Independent review must assess the
combined revision before any source grant.

## Decision: one privately owned runtime capability

The selected composition constructs one `*storage.RuntimeDB` with
`storage.OpenRuntime`. It passes that exact pointer to
`session.NewWithTxRunner` and retains it privately for every transaction that
calls that session service's `RecheckCurrentTx`. It retains the resulting exact
`*session.Service` privately too. Middleware and recheck use that same service,
not two services with equal configuration. The owning constructor is responsible
for cleanup on partial construction and shutdown.

The composition must not accept a separately supplied session service, arbitrary
`TxRunner`, external `*sql.Tx`, second database configuration, or callback that
selects a transaction source. It exposes HTTP behavior and lifecycle operations,
not these private fields or an authority constructor that accepts their pieces.
No business handler receives the session service or pool. Trusted authority code
obtains its transaction only as the argument of the retained runtime handle's
`WithTx` callback. All calls to the recheck method inside that composition must
be within that lexical callback and use its argument unchanged.

The selected profile does not reuse `apphost.NewLocal`: its `LocalConfig.Business`
receives development capabilities, and that host opens a separate pool for jobs.
Those existing development behaviors remain intact, but they are not the custody
proof for this runtime composition. A later host constructor and its exact file
ownership must be adopted separately. This decision adds no exported constructor,
transaction wrapper or context-injection API now.

This is a trusted-code invariant, not a property inferred from a transaction.
`RecheckCurrentTx` cannot distinguish a transaction on a copied database with
identical realm, person and session rows. Same-instance context provenance binds
the admission to the service object; it does not authenticate a transaction's
origin. Checking a realm row, database name, backend PID, connection address or
schema version would not turn an arbitrary transaction into this capability.
Do not advertise such checks as transaction attestation.

## SQL namespace and administrative trust

Existing session/store SQL contains unqualified object names. The selected
composition therefore requires a separately provisioned, immutable connection
profile whose effective object resolution is the reviewed installation schema.
For this profile the application schema is `public`, the configured search path
is `pg_catalog, public, pg_temp`, and the runtime identity has no database TEMP
privilege or CREATE/ownership privilege on either application or catalog schema.
It has no elevated or inheritable role path that restores those privileges.
Application schema objects, triggers, functions and grants are provisioned only
by the separately trusted migration owner. No untrusted role may create objects
in a searched schema. These are qualification prerequisites, not new runtime DDL
or permission to mutate an existing installation.

The constructor must qualify the supplied runtime role and deployment profile;
a successful `runtimeBindingReady` checks realm values only. It does not prove
these grants, schema definitions, trigger bodies, endpoint routing or migration
history. The trusted deployment must bind every pool connection to the same
intended database/schema. Independent connections to cloned databases behind a
routing endpoint violate this prerequisite even when all realm rows match.

The candidate recheck's new SQL must explicitly qualify application relations
with `public` and built-in database functions with `pg_catalog`. Existing producer
queries remain under the separately qualified search-path contract; this document
does not grant an unbounded rewrite of existing SQL. Callbacks must not execute
`SET ROLE`, `SET search_path`, session-setting functions, DDL or arbitrary SQL
that changes this binding. A generic SQL-capable plugin is not trusted authority
code. Runtime storage is a transaction capability, not a SQL sandbox.

PostgreSQL documents that unqualified names resolve through the search path and
that writable searched schemas are a trust boundary in its
[schema documentation](https://www.postgresql.org/docs/16/ddl-schemas.html#DDL-SCHEMAS-PATH).
Its [connection defaults](https://www.postgresql.org/docs/16/runtime-config-client.html#GUC-SEARCH-PATH)
describe catalog and temporary-schema resolution. Those rules motivate this
profile; they do not establish that an installation already meets it.

## Transaction and result lifetime

The authority transaction uses explicit READ COMMITTED with `ReadOnly: false`
and row locking enabled. The runtime role requires SELECT on all inspected
columns and UPDATE privilege on at least one column of each row-locked table,
as specified by PostgreSQL [SELECT privileges](https://www.postgresql.org/docs/16/sql-select.html).
A read-only product operation therefore does not imply a database read-only
transaction or a SELECT-only role. Grants must be provisioned and qualified
separately; this document does not broaden an existing role.
It receives the original bounded request context admitted by the exact session
middleware. The recheck result replaces the admitted principal in subsequent
trusted workspace/policy evaluation; passing the old context snapshot to a
consumer after recheck is forbidden. The original private context proof remains
unchanged for later rechecks.

Identity locks precede workspace/resource locks. A later recheck only reacquires
already-held compatible identity rows, resamples database time and returns the
current conservative assurance. The resource authority contract must specify
which predicates and minimum assurance to reevaluate after its own waits. No
`TransactionAuthorizer` signature or workspace resolver API change is adopted
by this document. The existing workspace middleware completes its own transaction
and therefore cannot be used as the same-transaction completion gate.

All selected data and rendered bytes remain provisional inside the owning
`WithTx` call. Failure of callback, cancellation, rollback, deferred constraints
or commit discards them. Return from the callback is not successful completion.
No headers, body, cookies, event, cache entry or external effect may publish a
provisional result. This rule does not by itself solve time expiry during
completion: the host's final sampling, permitted completion work and disclosure
linearization point still require their separate adopted contract and actual
schedules. No completion-freshness or full host claim follows from recheck.

## Finite evidence required before composed admission

These are required tests and review checks, not obtained results.

| Case | Required observation |
| --- | --- |
| Construction custody | Exact-head review traces the only runtime creation, the exact pointer passed into session construction, every recheck call and every close path. No exported method or configuration field permits replacing either private capability. |
| Two distinct services over one database | A context admitted by service A is rejected by service B before its recheck SQL. Equal configuration is insufficient. |
| Two independent databases with copied synthetic realm and identity rows | The actual composed host can only authenticate and authorize through its single retained handle; a second handle/transaction cannot enter its public surface. A direct low-level call using the wrong transaction is explicitly outside the trusted-caller contract, not a falsely expected primitive rejection. |
| Pool reconnection | Open several simultaneous connections and force an owned fixture connection replacement; every admitted connection uses the intended database, runtime role, search path and exact installed schema. Record the fixture's routing and provisioning scope. |
| Namespace admission | Wrong search path, searched-schema CREATE, database TEMP, elevated/inherited role access, missing schema, wrong binding and unreviewed migration history fail the relevant startup/qualification gate. No automatic repair. |
| Runtime immutability | Actual runtime credentials cannot create schema/temporary objects, alter binding or schema definitions, or assume an elevated role. Checks run in an isolated provisioned fixture with exact cleanup. |
| Fresh principal handoff | A later assurance expiry/downgrade reaches the consumer as the refreshed principal; an operation requiring the old elevation is denied. No stale principal is consumed after an Allowed result. |
| Completion | Real caller failure, cancellation and failed commit discard buffered output. Deferred-trigger waits and post-check expiry are tested under the future completion contract, not treated as already covered by this primitive. |

Negative mutations must include wiring a second capability into the composed
transaction path and passing the original principal after recheck. They must fail
for the intended assertion, then be restored before the independent passing run.
Static custody review complements those tests; a fixture with identical copied
rows cannot be used to claim the low-level API detects a wrong database.

The same-database design gate can be resolved by independently adopting this
closed composition rule. Composed admission still requires its implementation,
actual checks, complete producer/writer protocol, current workspace/resource
contract and completion protocol. No task count, product acceptance, fixture
permission, source assignment, deployment or provider qualification changes here.
