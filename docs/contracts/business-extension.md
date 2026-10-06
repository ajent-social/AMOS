# Business extension contract (design)

Status: design seam only; runtime operation registration is not implemented here.
This document records the landed v1.12 typed-handler design under ADR 023.
The v1.13 codec schema binding amendment under ADR 024 becomes frozen after
independent exact-head review and merge. It does not claim application startup
composition or runtime dispatch.

## Registration seam and ownership

The root integrator owns generated-application registry construction,
validation, and startup wiring. A business lane supplies operation definitions
and domain services; generated transport adapters bind validated definitions
to handlers. Registration is closed before serving. Duplicate operation IDs,
invalid typed bindings, unsupported features, missing dependencies, and route
conflicts are fatal validation or composition errors. They must not be silently
skipped or converted to successful placeholder handlers.

The design uses `OperationMetadata`, `OperationDefinition`, and
`FrozenRegistry` as semantic names only; this is not a declaration of a shared
Go API. Their required meaning is:

- `OperationMetadata` carries `operationId`, method, canonical path, typed
  input/output names, `permissions`, `entitlements`, `assurance`, `sideEffect`,
  `idempotency`, `mcp`, and declared features. Policy fields and enums exactly
  follow `api/policy.schema.json`; every field is required. Empty permissions
  or entitlements mean explicitly none. Collections are bounded and unique.
  This descriptor rejects ASCII control characters in every patterned
  string. The frozen `api/policy.schema.json` now has matching explicit
  control exclusions, so engines that match `$` before a final line feed still
  reject control-bearing operation and requirement names. The Go parity test
  checks that these exclusions agree. Runtime operation binding also supplies
  the immutable `Revision`, `InputSchemaDigest` and `OutputSchemaDigest` fields
  defined by the operation invocation contract. The digests identify the input
  and output codec schema artifacts; these are trusted runtime binding fields,
  not new caller-supplied fields in the business descriptor JSON schema.
- `OperationDefinition` binds that complete metadata to one validated typed
  handler and an explicit dependency set. Immutable input and output codecs
  expose `SchemaDigest() [32]byte`; `Bind` reads each once, rejects zero or
  metadata-mismatched digests, and snapshots the accepted values. Equality
  checks trusted binding consistency, not a dishonest codec's implementation.
  Future generated codecs derive the digest from the canonical schema artifact
  they implement and require independent generator fixtures. A generic `Bind[I,O]` semantic
  constructor accepts a non-nil
  `func(context.Context, operation.InvocationContext, I) (operation.Result[O], error)`, checks policy
  metadata and required dependencies, and creates the private erased invoke
  closure used to store heterogeneous operations. The context carries the
  trusted principal and current selection, resolved resources, invocation ID,
  transaction-scoped database wrapper, and bound effect enqueuer. The finite
  typed `Result[O]` carries one declared result kind and typed value; the
  executor validates and canonically encodes the output for replay. The closure
  decodes only into `I`; business handlers never receive an untyped map.
- Construction rejects missing metadata, nil handlers (including typed nil),
  missing required dependency keys, and required dependencies whose resolved
  value is nil. Policy validation and typed binding both complete before
  registration. Registration rechecks metadata/binding consistency, then
  rejects duplicate IDs, route collisions, and reserved routes.
- `FrozenRegistry` is a read-only snapshot. It exposes lookup by stable
  operation ID, accepts no further registrations, and cannot be modified
  through returned metadata or slices.

These semantics are a design target, not implemented declarations or runtime
evidence. They do not create or alter a shared `Principal`, evaluator, or error
API. The root integrator owns registry assembly and startup registration;
business packages do not self-register through package initialization or edit
root executable wiring.

### Path ownership

| Path | Ownership and lifecycle |
|---|---|
| `api/extensions/business.schema.json`, `api/policy.schema.json` | Authored contract schemas maintained by the root integrator; generators consume them and never rewrite them. |
| `internal/businessgen/**` | Authored AMOS generator implementation source assigned by T12.2; it is not generated output. |
| `templates/business/transport/**` | Authored AMOS generator template inputs assigned by T12.2; they are not generated application output. |
| `internal/amosgen/business/**` | Adopted generated transport output path inside a generated consumer application (ADR 021). Generated files are not present in this base. Only journal-managed outputs with verified preimages may be replaced. |
| `business/operations/**`, `business/types/**` | Application-owner authored handlers, services, and domain types. These are extension destinations, not files present in this base; generators must not create over or delete them. |
| `ui/overrides/**` | Application-owner authored UI overrides; compatibility checks preserve edits and stop on incompatible contracts. |
| Copied-once operation files | No copied-once operation path is designated. This extension does not copy operation source into an application; a future copy-once workflow requires a separately owned path and manifest before use. |

Before output, T12.2 must record managed output paths and preimage hashes, keep
authored and override paths outside its write set, and stop on ownership
conflicts. The table assigns lifecycle boundaries; it does not claim the
planned generator source, templates, generated consumer output, or runtime
registry already exist.

## Identity, policy, and errors

The adapter profile for the current public authority APIs is **person plus
resolved workspace selection**. After trusted authentication middleware,
`identity.PrincipalFromContext(ctx)` supplies the immutable
`identity.Principal`. After `workspace/context.Resolver.Middleware` has
revalidated the requested workspace against current rows, the same request
context supplies `workspace/context.Selection` through
`workspace/context.FromContext(ctx)`. The v1.12 typed adapter passes `InvocationContext` and typed input to the
handler. The context actor kind is derived from the trusted principal’s current
`Actor.Kind()` string and validated against the private initial `person`-only
profile; it does not add an `identity.ActorKind` type or accept caller-selected
identity fields.

The current `Principal` getters are `InstallationID`, `ApplicationID`,
`EnvironmentID`, `PersonID`, `SecurityEpoch`, `AuthenticationMethod`,
`AuthenticatedAt`, `Assurance`, `Actor`, and `Grants`. `Actor` exposes
`Kind`, `PersonID`, and `MachineID`; the current principal produces a person
actor, and `Grants()` currently returns nil. `Principal` has no workspace ID
or grant snapshot. The current workspace selection exposes `Workspace`,
`Membership`, `Permissions`, and `MembershipEpoch`; its permissions and
membership are request-time database values copied by `FromContext`, not
authority that business code may cache or synthesize. Machine workspace
resolution currently returns authorization-unavailable. The extension profile
therefore supports only the person path represented by these APIs.

Business code must consume both values from trusted middleware. It must not
construct a principal, actor, credential, workspace selection, membership,
grant, assurance value, or permission list, and must not infer workspace access
from a URL, header, route, or operation descriptor. Existing public types are
the adapter inputs for this profile. Any broader machine, grant, or workspace
principal surface needs an integrator-owned contract amendment and identity
implementation before an adapter can use it.

Authorization still requires a callable shared policy API. The planning
contract describes `policy.Evaluator`, but this base has no `policy` Go
package or evaluator implementation. The extension descriptor's permission,
entitlement, assurance, side-effect, idempotency, and MCP fields are
design-time metadata; they do not authorize invocation. A future adapter must
evaluate the trusted principal, resolved workspace selection, resource, and
requirements for each operation before calling a handler. Business handlers
may not substitute direct string checks against `Selection.Permissions` for
that evaluator. Until the integrator publishes and wires the policy adapter,
extensions must not claim runtime authorization or registered-operation
readiness.

`billing/catalog` is a narrower existing decision seam: it evaluates a
subscription feature and can combine that feature decision with a granted or
denied permission. Its catalog scope, subscription projection, and paid-feature
semantics do not cover arbitrary operation permissions, assurance, side
effects, idempotency, or MCP exposure, so it is not a substitute for the
shared evaluator.

### Remaining integration gates

ADR 021 accepts the person-plus-selection profile for extension design. The
planning contract's principal, actor, and grant types remain the broader target
surface; the accepted profile stages workspace authority in
`workspace/context.Selection` without changing current identity constructors
or claiming that broader surface is implemented. Machine workspace
authorization remains unavailable.

The root has adopted `internal/amosgen/business/**` as the managed consumer
output path, bound unsupported features to `extension.unsupported_feature`
with HTTP 422, and added matching control exclusions to the frozen policy
schema. The remaining runtime gate is a callable policy evaluator and a
composed registry that use the trusted principal and resolved selection,
validate every operation's requirements, and fail closed on unavailable
dependencies. No new business-facing policy package or API is declared here.

The shared error mapping is: missing/invalid authentication 401;
denial 403 (or non-enumerating 404 at a sensitive resource lookup); invalid
input 400/422; conflict 409; dependency or policy unavailability 503 with a
safe bounded retry hint only when known. Responses carry a stable
non-sensitive `code`, safe `message`, and server-generated request ID matching
the response header. Unsupported extension features use HTTP 422 and the
stable code `extension.unsupported_feature`; they never cause silent omission
or weaker behavior. The current `app` package has route-composition errors and
the current runtime emits request-scoped error responses, but there is no
shared application-error package that binds this extension code. The generator
and root composition layer must preserve this status/code mapping when they
add the runtime adapter; this design document does not claim that binding is
implemented.

## Canonical route rules

AMOS owns these case-insensitive prefixes, including descendants:

`/signin`, `/signup`, `/signout`, `/auth`, `/verify-email`,
`/forgot-password`, `/reset-password`, `/oauth`, `/.well-known`, `/account`,
`/workspaces`, `/billing`, `/api`, `/mcp`, `/healthz`, `/readyz`.

The current public composition seam is `app.New` followed by
`(*app.App).RegisterBusinessRoute(method, pattern, handler)`, before `Serve`.
Registration uppercases methods, normalizes a trailing slash, and rejects
malformed paths, backslashes, controls, repeated separators, dot segments, and
percent-encoded registrations. Reserved checks are case-insensitive and
segment bounded (`/apiary` is not `/api`). A terminal `/*` wildcard is
supported; any route under a reserved prefix returns `app.ErrReservedRoute`,
and overlapping routes for the same method return `app.ErrRouteConflict`.
Different methods may share an exact path. Current request matching decodes
one path-escape layer and rejects residual escapes and ambiguous forms.

These route checks happen during composition, before the server starts. The
current app does not load a business descriptor, construct an operation
registry, or defer descriptor failures to startup. The route tests below verify
the real public guard; they do not prove automatic business registry
composition. T12.2 must keep descriptor conflicts and unsupported registered
operations as explicit generation or composition errors before serving.

An upstream proxy, if selected by a future installation, remains a separate
private-origin adapter with exact allowlisting, credential/header stripping,
identity authentication, authorization, bounded redirects/host/body/timeouts,
and streaming constraints from the frozen contract. Proxy mode is unsupported
unless its complete adapter is registered and qualified; it is not inferred
from an operation declaration.

## Unsupported features and evidence boundary

Unknown registry fields, unbounded/ambiguous schemas, dynamic code loading,
implicit wildcard discovery, package-init registration, arbitrary principal
construction, policy bypass, unqualified proxying, and generated edits to
authored files are unsupported. They fail validation/generation explicitly;
runtime composition also fails closed if the required handler, dependency, or
shared-error mapping is absent. Unsupported-feature errors map to HTTP 422
with code `extension.unsupported_feature`; runtime response wiring remains an
integration gate as stated above.

`api/extensions/business.schema.json` validates design-time operation
descriptors. It is not runtime validation and does not replace OpenAPI
operation-policy validation. The Go fixture helper is explicitly test-only and
bounded; it mirrors the assigned schema constraints but is not a general JSON
Schema engine. Fixture and route-guard tests establish no production registry
behavior, provider behavior, startup composition, CI qualification, or task
acceptance.
