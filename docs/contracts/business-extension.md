# Business extension contract (design)

Status: design seam only; runtime operation registration is not implemented here.
This document applies to `amos-contract-v1`. It does not amend that frozen
contract, implement a registry, or claim application startup composition.

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
  This descriptor also rejects ASCII control characters in every patterned
  string. The frozen `api/policy.schema.json` uses `$` anchors that some JSON
  Schema engines match before a final line feed; its `operationId` and
  requirement patterns can therefore accept terminal line feeds. This
  descriptor applies the stricter control-free form without editing that
  shared frozen file. The inherited policy-schema anchor issue remains an
  integrator correction gate, and the Go test helper is written to reject the
  same control characters as this extension schema.
- `OperationDefinition` binds that complete metadata to one validated typed
  handler and an explicit dependency set. A generic `Bind[I,O]` semantic
  constructor accepts a non-nil
  `func(context.Context, identity.Principal, I) (O, error)`, checks policy
  metadata and required dependencies, and creates the private erased invoke
  closure used to store heterogeneous operations. That closure decodes only
  into `I`, invokes the typed handler, and encodes only `O`; business handlers
  never receive an untyped map.
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
| `internal/amosgen/business/**` | Proposed generated transport output path inside a generated consumer application. The path is not present in this base and must be adopted by the integrator before T12.2 emits files there. Only manifest-recorded generated files may be replaced. |
| `business/operations/**`, `business/types/**` | Application-owner authored handlers, services, and domain types. These are extension destinations, not files present in this base; generators must not create over or delete them. |
| `ui/overrides/**` | Application-owner authored UI overrides; compatibility checks preserve edits and stop on incompatible contracts. |
| Copied-once operation files | No copied-once operation path is designated. This extension does not copy operation source into an application; a future copy-once workflow requires a separately owned path and manifest before use. |

Before output, T12.2 must have the integrator adopt the proposed consumer output
path, record managed output paths and preimage hashes, keep authored and
override paths outside its write set, and stop on ownership conflicts. The table
assigns lifecycle boundaries; it does not claim the planned generator source,
templates, generated consumer output, or runtime registry already exist.

## Identity, policy, and errors

An adapter consumes a principal supplied by trusted AMOS authentication
middleware. It must not construct credentials, principals, grants, or assurance
values. The frozen target is `identity.PrincipalFromContext(ctx)` and the
immutable identity contract; business code does not add principal constructors.
The current `identity/contracts.go` does not yet match that frozen target
exactly: its principal exposes person/security-epoch/auth-method fields and
`Grants() []Grant`, while the frozen surface specifies actor/workspace/grant
semantics and other getters. Reconcile this under an integrator-reviewed
contract amendment or explicit API compatibility work before compiling the
future extension adapter; this task deliberately does not change shared types.

Authorization uses the shared `policy.Evaluator` contract from
`docs/planning/contracts.md`: evaluate the trusted principal, resource, and
requirements for every operation, including agent/MCP exposure. Business
handlers may not infer permission from route visibility or principal presence.
At this base SHA, no `policy` package exists, so this is a frozen semantic
reference and dependency gap, not a callable implementation.

The frozen shared error mapping is: missing/invalid authentication 401;
denial 403 (or non-enumerating 404 at a sensitive resource lookup); invalid
input 400/422; conflict 409; dependency or policy unavailability 503 with a
safe bounded retry hint only when known. Responses carry a stable
non-sensitive `code`, safe `message`, and server-generated request ID matching
the response header. Unsupported extension features are explicit validation
failures in the invalid-input 422 class; they never cause silent omission or
weaker behavior. The frozen contract does not define the shared code for this
unsupported-feature failure. The integrator must bind it to the shared code
vocabulary; constructor and runtime composition fail closed while that binding
is absent. A test-helper message is not a shared code. At this base SHA, the
`app` package aliases route-composition errors only and no shared application
error package exists. This is a documented integration gap, not permission to
invent a normative wire code.

## Canonical route rules

AMOS owns these case-insensitive prefixes, including descendants:

`/signin`, `/signup`, `/signout`, `/auth`, `/verify-email`,
`/forgot-password`, `/reset-password`, `/oauth`, `/.well-known`, `/account`,
`/workspaces`, `/billing`, `/api`, `/mcp`, `/healthz`, `/readyz`.

Normalize URL path before matching. Reject invalid UTF-8, controls, query or
fragment syntax in registrations, backslashes, duplicate separators, dot
segments, encoded separators, repeated percent decoding, and ambiguous
encodings. Canonicalize method to uppercase and path trailing slash to the
slash-free equivalent except `/`. Reserved checks are case-insensitive and
segment bounded (`/apiary` is not `/api`). Registration rejects same-method
canonical duplicate paths, exact paths covered by a registered terminal
prefix wildcard, nested/overlapping wildcards, and any route under a reserved
prefix. A wildcard is terminal `/*`; it cannot shadow a reserved route even
when registered first. Different methods may share an exact path, but method
normalization and wildcard overlap are checked independently for each method.
Catch-all patterns that cover an AMOS prefix are rejected. These rules describe
the composition contract; the current `app` route guard implements only the
behavior exercised by its tests and is the authority for runtime behavior.

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
shared-error mapping is absent. Unsupported-feature errors map to invalid
input 422, but the exact shared code remains an integrator decision as stated
above.

`api/extensions/business.schema.json` validates design-time operation
descriptors. It is not runtime validation and does not replace OpenAPI
operation-policy validation. The Go fixture helper is explicitly test-only and
bounded; it mirrors the assigned schema constraints but is not a general JSON
Schema engine. Fixture and route-guard tests establish no production registry
behavior, provider behavior, startup composition, CI qualification, or task
acceptance.
