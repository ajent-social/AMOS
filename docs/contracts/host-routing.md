# Finite host routing contract (v1.14)

Status: accepted design under ADR 025, frozen after independent exact-head
review and merge. INT-HOST-02 routing implementation is assigned separately. This is HTTP composition, not protocol
implementation, authorization, production hosting or database qualification.

## Public shape and ownership

The integrator owns `app/` exported aliases/options and `internal/runtime/`
implementation. Startup `Options` gains optional `Protocol *ProtocolHandlersV1`
and `Business *BusinessRoutesV1`. Each constructor copies the complete routing
configuration; later caller slice or field changes cannot alter selection.
Handler implementations are trusted application code and own their state.

```go
type ProtocolHandlersV1 struct {
    MCP http.Handler
    AuthorizationServerMetadata http.Handler
    ProtectedResourceMetadata http.Handler
    Register http.Handler
    Authorize http.Handler
    Consent http.Handler
    Token http.Handler
    Revoke http.Handler
}
type BusinessRouteV1 struct {
    Pattern string
    Handler http.Handler
}
type BusinessRoutesV1 struct {
    Home http.Handler
    Routes []BusinessRouteV1
}
```

A nil protocol bundle disables protocol binding. A present bundle requires all
eight non-nil handlers, including rejection of typed nil. Its exact slots are:

| Field | Path |
| --- | --- |
| MCP | `/mcp` |
| AuthorizationServerMetadata | `/.well-known/oauth-authorization-server` |
| ProtectedResourceMetadata | `/.well-known/oauth-protected-resource/mcp` |
| Register | `/oauth/register` |
| Authorize | `/oauth/authorize` |
| Consent | `/oauth/consent` |
| Token | `/oauth/token` |
| Revoke | `/oauth/revoke` |

No configurable paths or wildcard protocol bindings exist. Roots remain reserved
when handlers are absent; unknown descendants never fall through to business.
A selected slot receives every syntactically valid HTTP token method unchanged,
including case-sensitive extension methods. Protocol handlers own auth order,
status, response headers, body and Allow behavior. The host does not substitute
browser identity for protocol credentials or apply a universal auth wrapper.
Existing bounded telemetry still owns request IDs and sanitized access logs;
response equivalence tests allow only that documented request-ID decoration.

## Business routing and compatibility

A nil business bundle preserves legacy routing. A present bundle needs at least
one route or a non-nil home handler. Typed-nil configured handlers fail startup.
Home handles exactly `/`; no route entry may use `/` or `/*`. Without Home,
a legacy exact `/` route still works; otherwise current behavior is 404. Home
conflicts with any legacy exact `/` binding, regardless of method.

Route entries are exact canonical paths or one terminal `/*` prefix. No query,
fragment, escaping, dot segments, empty segments, trailing slash, backslash or
control character is allowed. Prefix `/records/*` matches `/records` and its
slash descendants, not `/records-archive`. Exact entries take precedence, then
the longest prefix. Duplicate patterns and any overlap with a reserved host
root fail construction, case-insensitively for reservation checks. Intentional
nested business prefixes and exact overrides are allowed within this new
manifest. A legacy method/path route may coexist only when its match set is
disjoint from every new business pattern, regardless of method. Legacy prefixes
retain their existing descendant-only matching semantics; no silent apex change.

Selected business handlers receive the original method, query, body and headers.
The host adds no method gate, synthetic identity or payload transformation.
An unmatched path remains 404. A valid path under an unbound reserved root keeps
the current sanitized unavailable response; there is no reserved fallback.

Protocol canonicality is strict: encoded aliases or trailing slash variants of
protocol paths return 400 and do not dispatch, whether the bundle is present or
absent. Malformed/ambiguous paths return 400; otherwise
unrecognized reserved paths return the existing unavailable response. Existing
legacy business normalization remains compatible. An encoded or trailing alias
must never normalize into a new manifest or protocol handler; aliases of new
business paths return 400. Reserved roots cannot be bypassed by escaping or case.
The router and telemetry template selection use the same match decision; only
fixed registered templates are logged, never concrete path values or queries.

## Freeze and authority

Registration and serving transition share one mutex. Routes freeze permanently
at the first `App.Handler()` exposure, valid `Serve` attempt, or direct internal
`Runtime.ServeHTTP` entry, whichever is first. Add `ErrRoutesFrozen` for valid
late registrations. Validate malformed/reserved input first for compatibility;
valid late registration then fails frozen. An invalid nil-context/listener Serve
call does not freeze. Shutdown or a failed listener does not reopen registration.
Concurrent registration either completes before the freeze or returns frozen;
no handler can observe a partially registered snapshot. Constructor validation
must finish before a listener can accept a request.

Route mounting grants no authority. Generated and registered AMOS operations
must use the v1.13 shared executor/policy contract; that executor is still gated.
Custom handlers retain explicit reviewed native session, CSRF/origin and resource
checks. Person identity alone never establishes ownership. No arbitrary route
registration is an alternate path to bypass the operation executor. This slice
does not add protocol authentication adapters or change session construction.

## Delivery and verification

The routing source assignment owns `app/app.go`, new routing tests under `app/`,
`internal/runtime/runtime.go` and new routing implementation/tests under
`internal/runtime/`. It changes no module, migration, identity or storage API.
Preserve existing telemetry, readiness, local host and legacy route tests.

Acceptance requires nil/empty/partial/typed-nil bundles; all eight slots; direct
handler equivalence for status, body, relevant headers, auth order and Allow;
GET/HEAD/POST/PATCH/DELETE/OPTIONS and mixed-case extension methods; original
query/body/header preservation; reserved/alias rejection; home, exact, longest
prefix, apex/sibling, duplicate/overlap and legacy behavior; immutable config
copies; deterministic freeze races and post-shutdown freeze. Use origin-form
RequestURI in handler tests and actual local HTTP requests for boundary cases.
Run relevant package tests, race checks, vet and lint under shared load/lease
rules. A genuine isolated alias/freeze-check mutation must fail its regression
and pass after restoring exact source. Independent exact-head review, merge and
landed verification are required; no source acceptance implies deployment.

Runtime-only PostgreSQL, runtime-role grants, TLS/root validation, production
origin/cookies/proxy policy, migrations and listener deployment remain separate
INT-HOST-02 stages. Routing does not depend on qualifying those stages first.
