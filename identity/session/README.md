# Session authority source

`NewWithWriter` retains the native ordering root and copied configuration. It
never creates a transaction runner, database pool, or credential producer.
`NewWithTxRunner` and the standalone issuance APIs remain Legacy-only. Profile
selection is permanent for the process; tests use subprocesses instead of resets.
The writer constructor does no database probe; the root enforces active ownership
when its `Run` or `Read` capability is used.

Native request admission binds the exact service and original bounded request.
`DiscoverPrior` runs before sealing and returns the old cookie's scoped session
and person, including a different person, plus a server-reserved new session for
issuing actions. Callers must union and deduplicate these rows with their full
native plan. Context-only admission requires the service's private committed
middleware proof and admits only MFA begin or password change; it cannot issue.
A public principal is never an admission capability.

`StageWriter` consumes one native issuance credential and buffers output inside
the transaction. `PublishWriter` requires the matching finalized permit and
single-use successful completion release. A failed/unknown commit or replay
releases no cookie or CSRF value. Native callers still own their complete final
predicate comparisons before finalizing evidence.

Writer middleware renewal and signout use the root's gate, complete discovered
person/session plan, ordered acquisitions, native actor evidence and final fence.
Middleware mints private reader admission only after authentication commit and
origin/CSRF checks. Admission retains no raw browser token. A downstream request
can establish its own native writer admission without reusing renewal's token.

`RecheckCurrentTx` requires the same service's private proof, matching public
snapshot when present, original cancellation/deadline and writable READ COMMITTED.
It separately locks person SHARE then session SHARE, samples database time after
the waits, and returns a refreshed principal through the existing identity bridge.
It does not renew, mutate, upgrade assurance, modify context or attest that an
arbitrary transaction belongs to the correct database. Private composition must
prove that custody. With optional assurance disabled, it reads no optional columns.

This is isolated source, not host admission or complete native producer
qualification. Pure and compile checks do not establish live database schedules.
`TestWriterRuntimeRequiredService` requires the trusted eleven-field
`AMOS_WRITER_RUNTIME_TEST_CONFIG` fixture and fails when it is missing. It runs in
an isolated process because Legacy selection cannot be reset. Its issuance seed
is explicitly synthetic; the test exercises native session persistence, renewal,
reader and signout, not native password verification. Independent exact-source
review, actual runtime checks, reader/writer wait schedules, complete native
callers and final predicates, private composition, and host admission remain
separate gates. The optional magic binding schema is not supplied by the current
writer fixture; later magic qualification needs a reviewed fixture extension.
