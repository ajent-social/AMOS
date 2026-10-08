# Root-owned delivery participants

`NewWriter(TxConfig)` copies and validates only material configuration and
cryptographic state. It owns no database or transaction runner, exposes no
resolver or pruner, and does not select the legacy writer profile. The existing
standalone `Store` retains its original transaction boundary and behavior.

A native W1 action uses the configured `internal/authoritywriter.Root` and a
sealed plan containing the exact material ID and job key. After acquisition
through W, call one of `PutVerificationWriter`, `PutSignInWriter`, or
`PutPasswordResetWriter`, then `email.EnqueueWriter` with a `sqlstore.TxWriter`.
Material insertion and job enqueue each consume their own single-use D permission
and record their mutation before SQL. The material reference, purpose, realm and
environment are validated; the job uses the same request validation, idempotency
and persistence implementation as the existing enqueue path.

A participant failure terminates the attempt with `Finish(UnavailableRollback)`
and returns its domain's unavailable error. Ignoring that error cannot restore
D permission or produce a successful completion. No savepoint or retry is added.
The native action must check all original challenge, material and job deadlines
and intended rows at the final database sample before successful Finish. These
participants do not implement that complete native action or finalizer.

`TestDeliveryWriterRuntimeRequiredService` is required-service **test source**
for ordering and durable intent. It uses one root activation per process and a
separately reviewed TLS runtime fixture selected by
`AMOS_WRITER_RUNTIME_TEST_CONFIG`. Missing configuration is a failure, not a
skip. Run it alone in a fresh process, with `-count=1`; the source author has not
run or qualified it. It creates no schema or fixture and cleans only its generated
realm's material and jobs. Its successful synthetic flows are not native identity
proof, full graph qualification, actual mail delivery or host qualification.

No dispatcher, provider, background resolver, pruner, claimant or scheduler is
constructed. The independent at-most-one effect dispatcher gate remains unchanged,
with zero dispatchers during quarantine. Provider, production and complete SaaS
qualification remain open.
