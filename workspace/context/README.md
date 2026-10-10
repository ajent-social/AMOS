# Current workspace facts in a caller transaction

`NewForTransactions(Config)` creates a resolver without opening or retaining a
pool. `ResolveCurrentTx(ctx, tx, principal, workspaceID)` accepts the caller's
explicit writable READ COMMITTED transaction. It returns a copied selection or
an entirely zero selection on error. The existing `New` and `Middleware` retain
their standalone behavior; the pool-free resolver cannot serve that middleware.

A zero selector discovers the unique personal workspace using the scoped
`personal_owner_id` relation. An explicit selector must be UUIDv7. Resolution
locks the scoped person SHARE, then the selected workspace SHARE, then an
organization membership SHARE, in separate statements. It checks the current
person state/security epoch, workspace state/epoch/owner, membership state/epoch,
and immutable role version. Missing roles, unsupported kinds/roles, invalid role
versions, malformed permission arrays and SQL/mode failures are unavailable.
Missing, foreign or inactive authority is denied. Invalid selectors are invalid.
The installed finite permission vocabulary is validated; unknown, duplicate,
null or empty grants are unavailable.

This is workspace data, **not session provenance**. The trusted caller must use
the principal just refreshed by its exact private session service on its private
runtime. Neither a supplied `sql.Tx` nor a supplied `Principal` attests database
custody. There is no credential parser, session context setter or pool accessor.
An organization selection also does not itself authorize a business operation.

Default selection must be pinned by its returned workspace ID for the completion
recheck. After downstream waits, recheck the session, resolve that pinned ID again,
and reevaluate policy using refreshed values. Compare workspace identity/epoch,
membership identity/epoch and permission set with provisional values. Drain all
deferred work before the final authority sample; commit successfully before
publishing any result. Rollback or commit failure invalidates provisional output.
This package does not own response publication or transaction completion.

The held person SHARE fences absence only under the **complete W1 writer graph**:
every relevant membership/person-binding writer must hold all affected person
rows UPDATE before workspace rows. A plain absence query does not lock a
predicate. Old incompatible writers must not coexist with this profile. No host,
resource admission, complete organization policy or product acceptance follows
from this source alone. See the [same-transaction contract](../../docs/contracts/current-workspace-read.md).

## Verification boundaries

`go test ./workspace/context -run '^TestCurrent' -count=1` exercises pure validation,
copy behavior and scripted SQL protocol/error handling. Scripts use the public
session constructor and middleware to produce synthetic principal data without
importing identity's internal proof package. They qualify neither PostgreSQL
locking nor any native credential producer. The legacy `TestT4_4_` suite retains
its separate isolated-schema prerequisites.

`go test ./workspace/context -run '^TestWorkspaceCurrentRuntime' -count=1` is
required-service test source for a separately qualified operator. It fails,
without skipping, if `AMOS_WORKSPACE_CURRENT_RUNTIME_TEST_CONFIG` is absent. The JSON
shape follows the repository's runtime TLS test profile (`host`, `port`,
`database`, `user`, `password`, `ca_path`, and existing diagnostic metadata).
It requires a precreated **public** identity/session/workspace schema, immutable
installed role versions, and a runtime-only role with the necessary scoped DML
and row-lock permissions. It never creates schema, changes role versions or
opens an administrator connection. Operator review and fresh fixture custody
must precede execution; the existing services profile is not presumed sufficient.

The source covers current default/explicit/organization selection, stale epochs,
inactive/removed membership, wrong mode, cancellation, closed/aborted transactions,
retained P/W/M locks, and a real observed writer-before-reader lock wait followed
by membership denial. Test session rows are synthetic. The finite test writer
uses P UPDATE before W UPDATE and membership mutation; it is not qualification of
the complete native writer graph. Malformed/missing role state is covered by pure
scripts; a real corrupt-state fixture needs its own operator-reviewed provision.

Independent exact-head source review, real normal/race execution, actual W1
writers in both schedules, predicate-creation/move exclusions, refreshed session
and selection propagation after expiry waits, same-database injection negatives,
and commit/no-disclosure integration remain separate gates. Passing pure checks
must never be reported as live-service or native-producer evidence.
