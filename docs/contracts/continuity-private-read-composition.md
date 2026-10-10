# Private continuity source-read composition

Status: finite B1 source proposal; independent exact-head design review is required
before implementation. This defines an internal composition component, not a public
host constructor, listener, installed migration registry, complete writer admission,
or completion of SEARCH/WEB/HOST or the full identity/provider gates.

## Ownership and private construction

The integrator owns `examples/continuity/host/core.go` and `read.go` and their tests.
All constructors and methods in this first component are unexported. A later public
host must separately qualify its complete selected writer graph and frozen HTTP,
configuration, registry and lifecycle contract before exposing these methods.

```go
type coreConfig struct {
    Database storage.RuntimeConfig
    InstallationID, ApplicationID, EnvironmentID uuid.UUID
    Origin string
    DevelopmentLoopback bool
}
type readCore struct { /* one privately owned runtime/root/session/resolver */ }
func openReadCore(context.Context, coreConfig) (*readCore, error)
func (*readCore) close() error

type sourceQuery struct {
    WorkspaceID uuid.UUID // zero selects current personal workspace
    ID string             // empty selects the list; nonempty selects detail
    After, Query, Kind string
    Limit int
    Fragment bool
}
func (*readCore) source(context.Context, sourceQuery) ([]byte, error)
```

`openReadCore` validates copied realm/origin/configuration, opens exactly one
`storage.RuntimeDB`, constructs one root from that exact pointer, one native
session service from that exact root, and one pool-free workspace resolver with
the same realm. It closes its owned database on every partial-construction failure.
It accepts no externally built pool/root/session/resolver/transaction, second DB
configuration, principal, policy callback or transaction runner. No credentials,
root, service or transaction are exposed by a public API. The private session
middleware is the only admission source for `source`; a context carrying merely a
public principal is insufficient. Immutable W1 activation remains process-wide;
this isolated component cannot coexist with legacy writers or claim that complete
writer qualification has been obtained simply because activation succeeded.

The component owns no listener, routes, middleware registration, schema installation,
provider calls, material resolver, dispatcher or application mutations. A public
host is not constructed here, and arbitrary application code cannot supply the
pieces of this private custody chain. Same-database custody is a reviewed construction
invariant, not a claim inferred from DB addresses, backend identifiers or equal rows.

## One read transaction and final authority

The source method uses only `Root.Read` and its original bounded context. Within
that exact writable READ COMMITTED transaction it calls the private session's
`RecheckCurrentTx`, then the existing resolver's `ResolveCurrentTx`. It permits only
the explicit personal-workspace source-read profile already defined in
`current-workspace-read.md`. The application constructs `repository.Scope` solely
from those refreshed realm/workspace values. No new owner lookup, credential parser,
search SQL, source model or principal constructor is added.

For a list, call existing `Repository.Sources` and `sourceview.RenderSources`; for
detail call existing `Repository.Source` and `sourceview.RenderSource`. Their
validation, literal search, scope, digest and maximum 512 KiB rendering bounds stay
intact. Input detail IDs cannot be combined with list-only selectors. All bytes are
provisional private memory; nil bytes are returned on every error.

After retrieval/rendering, run `SET CONSTRAINTS ALL IMMEDIATE` to drain deferred
work. Re-resolve using the pinned workspace ID while the original compatible P/W
locks remain held. Then perform the last session recheck. This last session-owned
DB time is the final authority instant; no SQL, domain callback or renderer follows
it. Pure checks compare the final selection's ID, realm, kind, state, owner and
epoch to the original pinned selection and reevaluate the personal read predicate
using the **last refreshed principal** and final selection. The earlier principal
is never used to grant final disclosure. The resolver's positive rows are held;
its absence guarantees still depend on the complete selected W1 writer graph.

Only after `Root.Read` returns successful commit and its cancellation check may
`source` return the buffered bytes. No database helper can publish response bytes.
Missing/foreign source and established authority denial return separate fixed
internal error classes for later non-enumerating HTTP mapping; SQL, corruption,
unsupported organization policy or unestablished authority is unavailable. A
semantic error crossing `Root.Read` uses its explicit `aw.ErrDenied` channel plus
a private fixed error classification; arbitrary errors must not be mistaken for
successful empty output or a preserved domain error.

## Required evidence and remaining gates

Independent review must trace the exact private constructor and all source calls,
verify complete cleanup, and show that neither a foreign session service nor an
external transaction can enter through the component's API. Required-service tests
use actual middleware admission and the qualified same-database fixture, including
native ordinary sign-in/personal selection/list/detail, literal search and digest
consistency, ordinary denial/no-output, failed transaction completion and canceled
original request. Component tests do not qualify a full host or provider. Any
operation currently blocked by platform review stays excluded and unverified;
this component is not a substitute test or a route around that restriction.

The later host still owes local listener/origin/cookie configuration, the installed
migration history and allocation, HTTP method/query/body/time limits and security
headers, complete identity routes, selected writer/provider qualification,
registered business mutation authority, full RB1-RB9 UI and RB10 restart/restore.
No new public host can be admitted while those applicable gates remain open.
