# Cross-lane semantic contracts

Date: 2026-09-30 UTC. Contract: `amos-contract-v1`. Status: frozen for implementation; foundation reconciliation reviewed. This is a design contract, not evidence that runtime code, providers, release gates, or certification exist. Amendments require an ADR, an integrator review, a version increment, and updates to affected task acceptance criteria before those tasks dispatch. Patch releases clarify wording without changing wire or storage meaning; any semantic or compatibility change increments the minor contract version and calls out migration needs.

## Ownership and package shapes

The integrator owns root module/dependency files, `api/amos.openapi.yaml`, shared identity and policy types, migration ordering and registry, executable entrypoints, runtime composition, generated registry wiring, and root CI. Product lanes own domain packages and submit OpenAPI fragments and migration proposals against this contract. Generated code imports exported packages only; `internal/` is private.

| Area | Public extension surface | Private implementation ownership |
|---|---|---|
| Runtime | `app/` composition and business handler registration | `internal/runtime/` wiring and lifecycle |
| Configuration | `config/` schema and validated immutable configuration | CLI setup/planner adapters |
| Identity and policy | `identity/` principal/authentication contracts; `policy/` evaluator contracts | Method adapters, durable stores, authorization query implementation |
| Workspaces | `workspace/` membership and authorization operations | Lifecycle, invitations, store adapters |
| Billing | `billing/` provider and entitlement contracts | Stripe adapter, durable intents, usage/seat reconciliation |
| Generated API | Domain files under `api/fragments/`; operation metadata in `api/policy.schema.json` | `internal/codegen/`, transport implementation, root bundle generation |
| Agent interface | `mcp/` adapter using a maintained SDK | OAuth/store integration and exposure registry |
| Business routing | `app/` registered integrated routes or `proxy/` private-service adapter | Header stripping, identity binding, origin allowlists |
| Presentation | `ui/` default views, theme, and override contract | Page rendering and browser behavior |
| Tooling | `cmd/amos/` stable commands | `internal/cli/`, templates and generation journal |
| Infrastructure | `infra/` owner-controlled programs/recipes | Provider-specific components and policy attachment |
| Operations | `observability/`, `operations/` contracts | Diagnostic pipeline, backups and recovery jobs |
| Maintenance | `maintenance/` job, verifier and policy contracts | Runtime adapters, evidence, rollout and upstream intake |

These names define ownership seams, not implemented packages or permission for multiple lanes to create competing models. A narrower task may own only its listed leaf files. No implementation lane owns `internal/` APIs as shared contracts.

Exported Go contract surfaces (proposed exact names, owned by the integrator when implemented):

```go
// package identity
type ID string // immutable, opaque, UUIDv7 text; never reused
type Principal struct {
    installationID ID; applicationID ID; environmentID ID
    actor Actor; workspaceID ID; grant Grant; assurance Assurance
    authenticatedAt time.Time
}
func (Principal) InstallationID() ID
func (Principal) ApplicationID() ID
func (Principal) EnvironmentID() ID
func (Principal) Actor() Actor
func (Principal) WorkspaceID() ID
func (Principal) Grant() Grant
func (Principal) Assurance() Assurance
func (Principal) AuthenticatedAt() time.Time
type Actor struct { kind ActorKind; personID ID; machineID ID }
func (Actor) Kind() ActorKind
func (Actor) PersonID() (ID, bool)
func (Actor) MachineID() (ID, bool)
type ActorKind string // "person" | "machine"
type Grant struct { id ID; scopes []string; revision string; expiresAt time.Time }
func (Grant) ID() ID
func (Grant) Scopes() []string // returns a copy
func (Grant) Revision() string
func (Grant) ExpiresAt() time.Time
type Assurance struct { level AssuranceLevel; methods []string; verifiedAt time.Time; expiresAt time.Time }
func (Assurance) Level() AssuranceLevel
func (Assurance) Methods() []string // returns a copy
func (Assurance) VerifiedAt() time.Time
func (Assurance) ExpiresAt() time.Time
type AssuranceLevel string // "aal1" | "aal2" | "aal3"
func PrincipalFromContext(context.Context) (Principal, bool) // reads trusted middleware context; no public constructor

// package policy
type Decision interface { isDecision() }
type Resource struct { Type string; ID identity.ID; WorkspaceID identity.ID }
type Allowed struct { PolicyRevision string; EvaluatedAt time.Time; ExpiresAt time.Time }
type Denied struct { Code string; PolicyRevision string; EvaluatedAt time.Time }
type Unavailable struct { Code string; Retryable bool; RetryAfter time.Duration }
type MCPExposure string // "never" | "eligible" | "challenge"
type Requirements struct { Permissions []string; Entitlements []string; Assurance identity.AssuranceLevel; MCPExposure MCPExposure }
type Evaluator interface { Evaluate(context.Context, identity.Principal, Resource, Requirements) Decision }

// package api metadata (serialized by api/policy.schema.json)
type OperationPolicy struct { OperationID string; Permissions []string; Entitlements []string; Assurance string; MCP MCPExposure; SideEffect string; Idempotency string }
```

Names and signatures above are the required semantic surface, not a claim that these Go declarations already exist. `Principal`, `Actor`, `Grant`, and `Assurance` have unexported fields, no exported constructors, and read-only getters; slice getters return copies. Only identity's internal verified-authentication plumbing can create and attach a principal. `PrincipalFromContext` is for consuming a principal that trusted middleware has already attached; arbitrary contexts and zero-value principals confer no authority. Business extensions consume the identity contract and do not implement credential proof or principal construction. Changes to exported meaning require an amendment. `ID` values are generated by trusted application code; parsing validates canonical lower-case UUIDv7 text, and callers cannot select IDs for authority-bearing records. Timestamps are UTC RFC 3339 with `Z`, stored as PostgreSQL `timestamptz`; database transaction time is authoritative for persisted `created_at`/`updated_at`. Do not use wall clocks for ordering concurrent mutations.

Internal auth adapters produce verified credential material after proof; business developers do not implement or construct credential values. `Principal` is immutable after trusted resolution and has exactly one actor branch. A grant contains the resolved permission scope snapshot and revision; current membership/grant state is rechecked at the authorization boundary. Assurance expires at `ExpiresAt`; `aal1` means a verified sign-in session, `aal2` means recent second-factor or equivalent phishing-resistant proof, and `aal3` means hardware-bound proof. Product tasks may require stricter assurance; they may not reinterpret these levels. The policy `Decision` is sealed to the three listed result variants. Stable codes use lowercase dotted names and must not embed resource existence, secrets, or provider text. `Unavailable` may carry a retry hint only when the evaluator knows that retry is safe.

Principal authority is created only after credential verification and current-state lookups by the server. Installation, application, environment, actor, workspace, grant, and assurance are distinct. Exactly one actor identity is present. Person and machine principals are disjoint. A selected workspace must be a personal workspace owned by the person or an organization workspace with currently active membership and applicable grant. Request headers, URL parameters, MCP arguments, API keys, email matches, and cached identity claims cannot construct or elevate a principal. API keys identify a machine credential and may narrow existing authority only. Every boundary rechecks revocation, membership, grant expiry, and resource tenancy against the current authoritative source; cache staleness must fail closed when freshness cannot be established. Personal workspaces are resource/billing containers, not global credentials; email equality never links identities.

## Policy, errors, and operations

An evaluation has exactly one result: `Allowed`, `Denied`, or `Unavailable`. Allowed includes policy revision and bounded validity; denied includes a stable non-sensitive reason code and policy revision; unavailable means the decision could not be safely established. Unavailable is never converted to allow or deny. `Denied` maps to HTTP 403, with 404/non-enumerating behavior at resource lookup boundaries when existence is sensitive. Missing/invalid authentication maps to 401. Unavailable maps to 503 and may include bounded `Retry-After`. Invalid input maps to 400/422, conflict to 409, and dependency unavailability to 503. Public error bodies contain stable `code`, human-safe `message`, and request `id`, never raw provider/database diagnostics. Sensitive identity flows use non-enumerating outcomes.

Every operation declares stable globally unique `operationId`, input and output schemas, permission and entitlement requirements (empty arrays mean explicitly none), minimum assurance, side-effect class (`read`, `write`, `external`), retry/idempotency semantics, and MCP exposure (`never`, `eligible`, `challenge`). Missing fields and unknown fields are rejected. The extension schema is `api/policy.schema.json`; it does not implement authorization. Eligible MCP operations still use the same application handler and policy evaluation as web and REST. `challenge` means the call returns a resumable human proof challenge; it does not bypass assurance. `never` excludes the operation from MCP discovery and invocation. Required permissions/entitlements/assurance cannot be inferred from prose or generator defaults.

Mutations must declare one of `required` (stable idempotency key, same key and request hash returns the original result; changed hash conflicts), `natural` (domain uniqueness makes retry safe), or `forbidden` (clients must not retry automatically). External effects use durable intent and reconciliation; unknown provider outcomes remain unknown until reconciled. Never promise exactly-once remote execution.

OpenAPI source is pinned to OpenAPI 3.1.1 as the compatibility baseline pending generator qualification in T2.1. The official specification identifies this version and its JSON Schema-based Schema Object dialect ([OpenAPI 3.1.1](https://spec.openapis.org/oas/v3.1.1.html)). OpenAPI 3.2.1 is newer, but its publication alone does not establish support in the selected Go toolchain, so it is not the frozen baseline. Keep root bundle at `api/amos.openapi.yaml`, domain fragments under `api/fragments/<domain>.yaml`; the integrator alone assembles and generates. Fragments cannot redefine security schemes, shared errors, servers, or reserved path roots. Generator and SDK versions must be pinned by the integrating task before code generation; this freeze does not imply generator qualification.

## Persistence and migrations

All durable entities use immutable UUIDv7 IDs and UTC `timestamptz`. IDs are never updated, recycled, or derived from email/provider identifiers. Domain lane owns its migration contents and rollback/forward-repair notes; integrator assigns a single monotonically increasing sequence, updates the registry, and owns cross-domain ordering. Migration application is serialized, one database transaction per migration, and the migration ID plus SHA-256 of exact source bytes are recorded atomically with schema changes; the record becomes visible only on successful commit. Nontransactional database operations are excluded from v1 migration scripts. An existing ID with a different checksum is a hard stop. Migrations are expand/contract by default; destructive cleanup, data loss, or external side effects require a separate owner gate and are not automated rollback. No lane edits the root migration registry or sequence without explicit assignment. Outbox records commit atomically with domain state; delivery is at-least-once and consumers deduplicate by immutable event ID. Retention requires domain and privacy review.

## Routes and extensions

AMOS owns `/signin`, `/signup`, `/signout`, `/auth`, `/verify-email`, `/forgot-password`, `/reset-password`, `/oauth`, `/.well-known`, `/account`, `/workspaces`, `/billing`, `/api`, `/mcp`, `/healthz`, and `/readyz`, including all descendants. These prefixes are reserved case-insensitively after URL path normalization; reject ambiguous encodings, dot segments, and duplicate separators before matching. Business routes register explicitly outside reserved prefixes; startup fails on duplicate normalized method/path, wildcard shadowing, or reserved-prefix collision. A separate upstream uses a private origin and exact route allowlist; strip credentials and client-supplied identity headers, authenticate propagated identity, and constrain redirects, forwarded host, timeout, body size, and streaming. Same-domain presentation does not confer trust.

Integrated business handlers call domain services directly. Upstream proxy adapters authenticate propagated identity and authorize before forwarding. Default UI pages consume application view models; overrides declare the UI contract version, are escaped at output, and cannot bypass the same policy checks. Upgrade tooling reports incompatible overrides and never overwrites them without an owner-controlled action.

## Configuration and providers

All Pulumi infrastructure programs and reusable infrastructure components used by AMOS are written in Go. TypeScript and Python infrastructure programs are outside the selected implementation stack.

`config/schema.json` is the canonical strict configuration schema. `mode` is explicitly `development` or `production`; development mode requires `localOnly: true` and a bind address of `localhost`, `127.0.0.1`, or `::1`. It does not relax user authentication. Deployment profiles are `aws_managed` and `aws_vm`; Cloudflare is a required provider, not a deployment profile. Provider availability is a runtime capability state (`available`/`unavailable`), never inferred from missing credentials as a successful no-op. A provider `state` of `required` means startup must resolve its credential reference and readiness fails if that capability is unavailable; required providers must include a credential reference. Production requires AWS and Cloudflare. Development may explicitly set AWS and Cloudflare to `disabled` and run locally without cloud credentials. Cloudflare's `proxyMode` (`dns_only` or `proxied`) is required when Cloudflare is required. `disabled` is an explicit absent capability and callers return an unavailable result for operations that depend on it. Email, payment, and identity capabilities may be explicitly disabled where the installation stage permits it.

AWS choices are the two confirmed deployment shapes: managed containers with managed PostgreSQL (`aws_managed`) and one VM hosting app plus PostgreSQL (`aws_vm`). Cloudflare is required by product scope but exact DNS/proxy behavior remains unqualified; `proxied` remains unavailable until qualified. This contract does not choose account resources or authorize spending. Secrets are represented only by references (`env://NAME`, a local file reference, or provider secret reference objects); literal credential-bearing values are prohibited. Values in schema examples/fixtures are synthetic. Missing/invalid secret references yield explicit configuration/provider unavailable errors and never trigger anonymous or weaker fallback.

There is no mandatory hosted control plane. Deployment state, artifact/source identity, credential scope, schema compatibility, release receipts, and recovery evidence are installation-owner controlled. Choosing a profile is an explicit migration decision, never an automatic config toggle. Cloud accounts, DNS, certificates, email, payments, and identity-provider setup remain external owner gates.

## Privacy, maintenance, and compatibility seams

Diagnostics crossing an installation boundary use allowlisted, bounded, structured fields; exclude raw logs, business data, secrets, identifying infrastructure details, and unreviewed source paths. Synthetic reproductions and private vulnerability handling remain separate from product telemetry. Owners can inspect, disable, and set retention for reporting. Maintenance plans bind immutable evidence, source/artifact identity, policy version, time/spend/retry limits, egress boundary, rollback eligibility, and manual suspension. Proposer, verifier, and releaser authority stay separate; coding jobs have no production credentials and cannot alter acceptance or release policy. No contract here grants autonomous production deployment authority.

The integrator maintains the compatibility matrix for core, UI, generator, configuration, API, database, and agent-client versions. OpenAPI generation or schema validation alone does not prove protocol conformance. Dependent lanes add scoped behavior and denied-path tests; integration proves the registered boundary, persistence behavior, and intended provider tier. A missing required service is a visible failure, not a skipped pass. Provider fixtures cannot satisfy provider gates.

## Deferred decisions and evidence

The project license is Apache-2.0, selected by the owner. AWS, Cloudflare, email, payment, identity-provider, and database/session implementations retain their owner/provider qualification gates. This contract, local schema validation, and synthetic fixtures are design evidence only; they do not establish provider behavior, deployment, release, security certification, or production readiness. Root review is pending.

## Contract amendment v1.1

Reserve all selected authentication and protocol discovery route roots before business registration. Runtime errors carry a server-generated request ID that agrees with the response header; client-supplied correlation headers confer no authority and are not reused as trusted IDs. Reject control characters, invalid UTF-8, and repeated percent decoding at ingress. Task T2.2 includes actual rejection tests; downstream route and generator tasks consume this revision.

## Contract clarification v1.2: pending registration

ADR 010 defines the transaction-bound `identity/store.PendingRegistration` and
`workspace/personal.BootstrapPending` composition seam. Only successful creation
of a pending account mints this immutable proof; the workspace participant checks
the same transaction, persisted pending state and scoped owner. It grants no
principal/session authority. Existing active-principal bootstrap stays intact.
Signup uses natural uniqueness idempotency and generic responses; duplicate
registration cannot change credentials, ownership or grant a session. T3.5 must
verify this composed boundary, including rollback and concurrent retry.

## Contract clarification v1.3: authentication route composition

ADR 012 defines the finite startup `app.IdentityHandlers` seam. It binds exact
implemented authentication routes while preserving business prefix reservations,
built-in health/readiness and ingress normalization. Nil handlers remain
unavailable; supplying one grants no authentication authority or CSRF exemption.

## Contract amendment v1.4: explicit loopback email evaluation

ADR 013 permits HTTP email action origins only with an explicit
`DevelopmentLoopback` configuration and canonical loopback host. The default and
remote origins remain HTTPS-only. Generated composition must validate local-only
development mode and listener before enabling this flag; a local capture transport
is a distinct, visibly unqualified capability. No production migration is needed.

## Amendment v1.5: executable migration order

ADR 014 fixes the embedded initial foundation order at sequences 1 through 7 and
application fragments at 8 and later. Published migration IDs and SQL bytes are
immutable; upgrades append entries. The reference todo fragment owns sequence 8
in the reference application. Current-person personal workspace selection is
realm-scoped and does not grant authority from identifiers.
