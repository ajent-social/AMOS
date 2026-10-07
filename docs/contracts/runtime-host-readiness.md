# Read-only runtime binding readiness (v1.20 proposal)

Status: proposed; independent exact-head review and landing are required before
source dispatch. This is one production-host prerequisite, not a production
constructor, schema certification, current authority or deployment acceptance.

## Purpose and boundary

The development host's `Host.bind` may create `amos_runtime_binding`. A runtime
host must instead require the existing deployment identity, using only the
runtime connection and SELECT privileges. Reusing `NewLocal`, its binding
initializer or its migration-capable database handle is prohibited.

The coordinator owns this additive private `apphost` helper:

```go
func runtimeBindingReady(ctx context.Context, db storage.TxRunner,
    installation, application, environment uuid.UUID) error
```

The source defines private `errRuntimeBindingConfiguration` and
`errRuntimeBindingUnavailable` sentinels. No exported host constructor, handler,
credential resolver, executor or raw database accessor is added by this slice.
The future constructor maps private errors to its separately reviewed public
startup/readiness error contract. Existing `NewLocal` behavior remains intact.

## Exact behavior

- Reject nil context, nil or typed-nil runner and invalid UUIDv7 RFC4122 realm IDs
  with the configuration sentinel before calling the runner. Accept non-nil value
  implementations. Do not probe, close or assert a richer storage capability.
- Derive a context bounded to two seconds; an earlier caller deadline wins. Use
  exactly one `WithTx` call with explicit READ COMMITTED and `ReadOnly: true`.
- Read all three realm IDs from the single `singleton = true` row in
  `public.amos_runtime_binding`. Use a fixed schema-qualified query, no dynamic
  identifiers, database lookup, caller SQL or session-setting mutation.
- A missing row or any differing realm ID yields the configuration sentinel.
  Return success only if all three match AND the owning runner completes
  successfully. A successful query callback cannot override failed completion.
- Other database/transaction failures yield only the unavailable sentinel,
  joined with `context.Canceled` or `context.DeadlineExceeded` if the bounded
  operation context has ended. Never expose SQL, connection diagnostics, realm
  IDs, credentials or raw runner errors. Missing/wrong schema is unavailable,
  not permission to create or repair schema.
- When the runner returns the helper's own missing/mismatch error unchanged,
  retain configuration classification. If transaction cleanup fails or a runner
  returns an error joined with a configuration error, classify unavailable;
  never hide infrastructure failure behind configuration classification.
- Do not catch arbitrary runner panics. RuntimeDB transaction panic/rollback
  semantics remain those of v1.15. No retry, DML, migration, advisory lock,
  binding insert, schema ownership change or provider call is permitted.

The immutable binding table/trigger from the existing RuntimeBinding fragment
must be precreated by a separate authorized provisioning path, outside runtime
credentials. This helper verifies the stored realm only. It does not prove the
rest of the schema, all rows' tenant isolation, role grants, immutable trigger
presence, protocol policy or production endpoint identity. Those remain separate
composed acceptance gates. The runtime role must have SELECT but no INSERT,
UPDATE, DELETE, TRUNCATE, ownership or schema-management rights on the binding.

## Source assignment and evidence

After design landing, the explicitly assigned source author owns only new files
`apphost/runtime_binding.go`, `apphost/runtime_binding_test.go` and
`apphost/runtime_binding_integration_test.go`. No edit to local host, binding
initializer, migrations, module files, generator or executable is delegated.

Offline checks prove nil/typed-nil admission without calls, valid value runners,
exact options and bounded context, safe error classification, earlier deadlines,
failed completion, and no transaction ownership methods. These checks do not
substitute for the query executing against PostgreSQL.

Required actual TLS PostgreSQL tests use separately provisioned finite profiles:
BOUND contains the exact immutable RuntimeBinding fragment and one fixed
synthetic realm; EMPTY has the same schema without a binding; MISSING has no
binding table. Runtime consumers have SELECT on the binding only where present,
plus the usual harmless runtime probe table. They never receive admin credentials.
The BOUND profile proves exact match, each realm mismatch, repeated read-only
operation, cancellation, closed handle and a real successful callback followed
by deliberate failed completion. EMPTY proves missing-binding rejection;
MISSING proves missing-schema rejection. Each profile's role must demonstrably
deny binding writes and administrative operations. The operator records actual
controls; test files neither create nor repair schema. Missing configuration
fails visibly with zero skips.

Independent exact-head review repeats normal/race/vet/pinned lint, actual profile
checks and a meaningful isolated negative/restored test that removes the realm
comparison or suppresses completion failure. Guarded merge requires fresh
head/base/claim/check readback; landed source bytes and fresh actual checks are
recorded separately. Fixture success is only local evidence, never provider or
production qualification.
