# W1 ordering root

This package implements the ordering portion of the reviewed
[writer protocol](../../docs/contracts/current-session-writer-api.md). It is
trusted native plumbing, not a credential constructor or SQL sandbox. The
single private runtime remains owned by the composition, including shutdown.

Plans copy and order fixed table/UUID inventories. Attempts enforce the frozen
process profile, original deadline and private context token, phase progression,
row membership, finite delivery steps and the counter-only mutation journal.
Root acquisition begins with the database singleton. Only successful commit
creates a completion; one atomic terminal release survives attempt closure.
Native identity publication additionally requires its separately owned final
permit. Calling Finish does not prove any credential or policy predicate.

Native adapters must discover the complete affected person/row set, obey every
mutation journal call, compare current method evidence after all waits, and
never retain or misuse a raw transaction. An ordering package cannot inspect
arbitrary SQL or infer that those native obligations were met. Legacy native
constructors and every selected writer still need integration with this process
profile before any composed W1 host can be admitted. This package alone neither
stops an old executable nor qualifies a deployment or provider.

Pure tests exercise immutable plans, process-profile exclusion, cancellation
bounds and concurrent one-shot terminal release. The required-service test
requires `AMOS_WRITER_RUNTIME_TEST_CONFIG`, an operator-owned TLS runtime profile
with the separately provisioned singleton and reviewed authority schema. It
fails when that prerequisite is missing. Test setup touches only unique synthetic
rows; runtime credentials cannot create or repair the singleton. The initial
runtime schedules cover root rollback/poison/lifetime, reserved insertion, mode,
least-privilege gate grants and cancellation while the gate is held. They do not
replace the complete native producer, child-row ordering, deferred constraint,
counter transition, delivery, reader/writer and W01–W28 integration schedules.

The additive operation profile has an exact copied person/session/workspace plan
and finite native steps within D. It forbids mixing identity mutation, delivery
or counter paths, requires every open step to finish before the final drain, and
binds new/replay/audit-only terminal outcomes to their respective paths. Its raw
transaction is trusted native plumbing only; operation callbacks still require
the guarded DBTX. A journal transition proves no policy decision or SQL effect.
See [the exact contract](../../docs/contracts/operation-writer-journal.md).

Post-claim denial remains an explicit integration gap: this profile can only
roll back, without claiming an audit-only receipt. Full operation dispatch is
blocked pending the atomic denial protocol, native current-authority/final
receipts, complete billing/resource locking, callback/replay/effect/audit wiring
and real-service qualification. The source's synthetic transaction tests execute
no SQL and qualify only the protocol's in-memory paths and rejection guards.
