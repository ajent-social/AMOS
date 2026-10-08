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
