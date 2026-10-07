# Private handler completion boundary (v1.19)

Status: adopted after independent exact-head design review and PR41 landing.
This is a private prerequisite under T2.8, not an executor or authorization API.
It preserves v1.18 SQL admission, lifetime and drain behavior.

## Source boundary

The existing private `Definition.invoke` calls the handler and then output codecs.
Invalidating around that entire call leaves retained database handles usable by
codec code. Split the internal typed binding into handler and result completion
stages, without changing `Bind`, any exported type shape or registry metadata.
The private handler stage returns the complete `Result[O]` wrapped as `any`,
not its bare output value. This preserves valid nil-interface output values
without losing their generic type. The handler closure validates its input type
and receives the existing cloned invocation context. The completion closure
asserts the complete `Result[O]`, checks result kind, encodes its Value, validates
and copies canonical output as before.

Preserve the existing private invoke behavior by composing the two stages, so
existing registry tests retain their meaning. The future guarded invocation path
must use the new private helper below, never the compatibility closure. Registry
construction rejects definitions missing either stage as well as the existing
required closures. No public invocation entry point is introduced.

## Guarded helper

Add an unexported helper accepting context, a bound Definition, an
InvocationContext, typed input as `any`, and the owner-supplied `*sql.Tx`.
It returns `CachedResult, error`. It rejects nil context, nil transaction and
incomplete definitions before executing a handler. It opens no transaction,
connection or pool and performs no commit, rollback, retry, policy evaluation,
audit write, replay lookup or effect enqueue itself.

Construct the existing v1.18 callback database guard for exactly that transaction
and replace the copied invocation's DB with it; ignore any DB supplied in the
input invocation. Do not expose the invalidation closure to the handler. Other
invocation fields retain existing defensive-copy semantics. They remain trusted
future-executor inputs, not evidence of current authority.

Run the handler stage in an inner function whose defer always invokes invalidation.
That inner function must return only after actual invalidation/drain completes.
Normal handler success proceeds to result completion only if invalidation returns
nil. A handler error returns no result; preserve its identity when cleanup succeeds,
and join handler and cleanup errors when both fail. Cleanup failure after handler
success returns the cleanup error and no result. Invalid input types also close
the created guard and execute no codec. An unfinished result therefore prevents
output completion and requires the future owner to roll back the transaction.

On handler panic, invalidate/drain and rethrow the exact panic value. Cleanup
errors must not replace the original panic. No cleanup goroutine may be abandoned
to pretend the callback has drained. The guard's supported-driver and cooperative
Scanner assumptions remain unchanged; this is not a general bounded-cancellation
promise. A panic or error from result-kind checking, encoding or validation occurs
only after invalidation; retained DBTX, Row and Rows are already closed.

The helper returns private errors to its future owner. It does not map HTTP or
policy outcomes. Even a successful helper result does not establish a committed
transaction; the eventual executor must suppress results on commit failure.

## Verification and ownership

Following independent design review and landing, the assigned source author owns
`app/operation/operation.go`, new `callback_invocation.go` and
`callback_invocation_test.go`. Existing SQL guard files, public APIs, schema,
module files, policy and transport wiring remain outside that leaf.

Normal and race tests must exercise handler success, error, exact-value panic,
invalid input/result types, valid nil-interface outputs, invalid result kind, output encode/validate errors and
panics. Codec probes must observe closed retained DBTX/Row/Rows, unchanged Scan
destinations and no driver access. No codec may run after handler or cleanup
failure. Exercise unfinished rows, cleanup failure, joined error identity and
actual drain before completion, with bounded barriers rather than sleeps.

A required real TLS PostgreSQL test uses the reviewed runtime fixture and one
owner transaction: completed DML plus handler return invalidates before codec,
owner commit persists; unfinished rows prevent completion and owner rollback
removes writes; a handler panic closes retained handles before exact rethrow and
owner rollback. Missing service prerequisites fail visibly. These checks do not
qualify a production role or an operation executor.

Independent review must reproduce a mutation moving invalidation after codec
execution and observe the intended retained-handle assertion fail, then restore
exact bytes and repeat passing checks. Scoped normal/race/vet/lint, independent
exact-head review, guarded merge and fresh landed service checks are required.
Current authority, refreshed-principal handoff, complete writer lock order,
replay/capacity persistence, audit/effect and transport gates remain open.
