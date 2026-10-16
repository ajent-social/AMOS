# Bounded optional tracing

Proposed amendment v1.33, T10.5. Independent design review and landing precede
source dispatch. This is an additive, explicitly enabled owner-operated tracing
component, not a collector deployment, production constructor or provider claim.
T10.3 logging/correlation and T10.4 metrics retain their contracts. Tracing is
never an application readiness dependency or a source of application authority.

## Public composition and configuration

Add a public `observability` facade with these initial shapes. Its types expose
standard library configuration and finite records, never SDK providers, spans,
processors, exporters, raw transports or mutation callbacks:

```go
type TraceConfig struct {
    Enabled bool
    SamplePerMillion uint32
    RouteTemplates []string
    Collector TraceCollectorConfig
    Outbound []TraceDestinationConfig
    TrustedIngressSPKI [][]byte
}
type TraceCollectorConfig struct {
    Authority, DialIP string
    CAPEM []byte
    AuthToken string
}
type TraceDestinationConfig struct {
    Authority, DialIP string
    CAPEM, ClientCertificatePEM, ClientPrivateKeyPEM []byte
}
type TraceStats struct {
    Attempted, Exported, Dropped, Failed uint64
    Active, Queued uint32
}
func NewTracer(TraceConfig) (*Tracer, error)
func (*Tracer) Transport(destination int) (*TraceTransport, error)
func (*TraceTransport) RoundTrip(*http.Request) (*http.Response, error)
func (*TraceTransport) CloseIdleConnections()
func (*Tracer) Stats() TraceStats
func (*Tracer) Close(context.Context) error
```

`app.Options.Tracing *observability.Tracer` and optional
`apphost.LocalConfig.Tracing *observability.Tracer` forward the same caller-owned
instance. Host close does not close a shared tracer. The installation owner calls
bounded `Tracer.Close` after stopping its application hosts. No default provider,
collector, network target, token, certificate, sample rate or deployment is chosen.
Nil/default-disabled tracing creates no exporter, goroutine or network operation.
Disabled construction accepts only the zero configuration; populated disabled
configuration fails rather than ambiguously retaining credentials. Explicit enabled
configuration validates fully before starting its one worker; construction does
not dial a collector or perform readiness network I/O. Enabled ratio zero is valid
and records no spans. Reject every `OTEL_*` environment variable when enabled,
including empty-valued ones: SDK/environment defaults are not operator approval.
No endpoint, header, authentication, proxy, TLS or sampling environment fallback.

Copy every slice and parse fresh CA/client-key material. Maximum configuration:
32 unique route templates, 16 unique destination authorities, 16 unique ingress
SPKI SHA-256 digests of exactly32 bytes, 64KiB per PEM input and 512 bytes for the
nonempty dedicated collector token. Token grammar is visible ASCII without space,
control, CR or LF. Route grammar/256-byte limit follows bounded metrics; configured
names remain local and become only ordinal `r00`–`r31`. Destination order yields
`o00`–`o15`; private authorities, IPs and certificates never become trace fields.
Sampling is an explicit integer0–1,000,000. Reject duplicate, malformed, oversized
or unsupported configuration with generic errors that omit supplied values.

## Private SDK and fixed observation vocabulary

Use the already pinned OpenTelemetry Go SDK1.45.0 and OTLP protobuf1.11.0.
The existing gRPC1.83.2 pin is unchanged but is not used for collector transport. No version change or auto-instrumentation dependency. Construct a private
SDK provider with explicit fixed resource and span limits, never global setters,
global providers, global propagators, default detectors or an SDK batch exporter.
The owned nonblocking processor/worker described below is the sole export path.
SDK limits explicitly permit at most2 attributes per span,16 bytes per attribute
string, zero events, zero links, zero event/link attributes. Use the pinned SDK's
raw-limit option where zero must mean zero, not a default; test those semantics.
An independent admission gate caps all simultaneously recording server, database
and client spans combined at256, before allocating SDK recording state. Release
admission on every normal/error/cancel/panic exit. Capacity failure drops only
observation and increments a saturating numeric counter.

Finite names and kinds are `http.server`/SERVER,
`database.transaction`/INTERNAL and `http.client`/CLIENT. HTTP server attributes
are `amos.route` (configured ordinal, `other` or `unmatched`) and `amos.outcome`;
HTTP client attributes are `amos.destination` (ordinal) and `amos.outcome`;
database attributes contain only `amos.outcome`. Outcomes are `ok`, `error`,
`cancelled`, `panic`, `commit`, `rollback` or `unavailable`, restricted by this table:

| Kind | Allowed outcomes | Classification |
| --- | --- | --- |
| Server | ok, error, cancelled, panic | Panic first; then cancelled context; then sanitized1xx–3xx success versus4xx/5xx/other error. |
| Client | ok, error, cancelled, unavailable | Cancelled context first; transport failure without response is unavailable; sanitized status classes otherwise follow server classification. |
| Transaction | commit, rollback, error, cancelled, panic, unavailable | Panic first; cancelled context next; failed admission/begin is unavailable; successful commit is commit; callback error with successful rollback is rollback; commit/rollback failure is error. |

No individual SQL, driver diagnostic or application error string affects labels.
Database outcomes reflect actual completion, not intended work. No error value/string or status description is attached.
Status code is UNSET for successful outcomes and ERROR otherwise, with empty
message. The wire contains IDs, parent ID, local timestamps, kind, fixed name,
these finite attributes and local sampling flag only (wire flags0 or1). No HTTP method/URL/path,
SQL text/parameter, row, session, billing, workspace, user, provider, credential,
header, body, exception, event, link, baggage, process, host or environment data.

Pass application/business code only a nonrecording SDK SpanContext in its context
plus a private opaque recording-scope value under an unexported key. Never install
a writable SDK recording span in any context passed outside telemetry internals.
`trace.SpanFromContext(ctx)` mutation, error recording, events and links from a
business handler must be ineffective. Keep private recording handles inaccessible
through public return types and callbacks. Ignore caller-installed SDK spans,
baggage and global-provider state when selecting recording authority. Preserve
request cancellation and unrelated business/auth context values. Place the same App
observation wrapper under both existing logging/metrics dispatch paths and inside
existing panic recovery (or an equivalent private rethrowing boundary), so a panic ends a panic-labelled span and the original panic
still reaches existing application recovery. Existing request-ID/logger shortcuts
never establish trace trust or bypass fresh ingress selection.

The private end processor reconstructs a fresh finite record rather than retaining
an SDK ReadOnlySpan or its attributes/resource/scope. Immediately before protobuf
serialization reconstruct the resource, scope and every span again from allowlisted
typed fields. Unknown/invalid records are dropped, never regex-redacted. Resource
is exactly `service.name=amos`; instrumentation scope name is exactly
`amos.tracing.v1`, with empty version/schema URL and no attributes. All other
resource/scope/span fields, including SDK defaults and unknown protobuf fields,
are absent/default. Empty error/status messages, events and links are enforced
at this second boundary even when upstream recording state is poisoned.

Trace IDs, timing, topology and ordinal activity can reveal operating patterns.
This is minimized diagnostic telemetry, not an anonymity or non-linkability
promise. Collector retention/access/deployment is an independent owner gate.

## Ingress sampling and observation-only continuity

Every public/untrusted ingress starts a fresh cryptographically random trace ID
and span ID. Ignore incoming `traceparent`, `tracestate`, `baggage` and caller SDK
context. Local sampling uses fresh local randomness and the configured ratio;
it is independent of incoming sampled bits and incoming trace-ID values. Random
source failure denies recording/continuation generically rather than weakening ID
creation. Make this root sampling decision once and store it immutably in the opaque
local scope. All local server/database/client children inherit it, with no
independent child draw. The private SDK sampler reads this owned decision, never
ParentBasedAlwaysSample or caller SDK context. Capacity drops can remove individual
records but cannot increase sampling. All emitted sampled flags reflect the
effective owned decision, never a caller request for observation.

Optional continuation is admitted only when the actual request TLS connection
has completed TLS1.3 and
contains normally verified client certificate chains whose leaf equals the actual
peer leaf, and the verified leaf's SPKI
SHA-256 matches a copied finite allowlist. Headers, booleans, forwarding metadata,
client-presented unverified certificates and callbacks cannot establish trust.
Trusted ingress still requires exactly one canonical lowercase W3C version00
`traceparent`:55 characters, fixed separators, nonzero32-hex trace ID/nonzero16-hex
parent ID and flags exactly00 or01. Reject aliases/duplicate case-variant header
keys, multiple values, comma joins, surrounding whitespace, future versions and
trailing bytes. Invalid/absent propagation starts a fresh trace; it does not reject
an otherwise valid business request. Drop all tracestate/baggage and do not copy
unknown propagation flags. Trusted continuation preserves trace/parent IDs and
may only narrow observation: incoming00 forces an unsampled effective decision;
incoming01 cannot override a negative local root decision. Untrusted incoming
flags never narrow or expand local public-ingress sampling. Local randomness
remains independent; the immutable effective decision propagates to all local
children. Verification is checked per request, including
normal certificate validity, rather than accepting stale/forged TLS state.

A separate private backend may serve `App.Handler()` from a standard TLS1.3
`http.Server` configured with `RequireAndVerifyClientCert`, copied dedicated trust
and the expected peer allowlist. This does not broaden NewDevelopmentTLS, add
application identity envelopes or qualify production mTLS. Trace continuation
never authenticates a person, workspace, API key, bearer token or entitlement;
normal App/business authorization remains independently required.

## Real database boundaries and outbound HTTP

Instrument both `storage.DB.WithTx` and `storage.RuntimeDB.WithTx` only when their
context has an owned active tracing scope. Cover begin, callback, commit and
rollback completion; do not instrument individual SQL calls or migration paths.
Keep their callback signatures, transaction ordering, error wrapping/identity,
context cancellation and original panic value unchanged. Telemetry must never
recover a panic as success, change rollback/commit behavior or return its own error
in place of the application's result. Storage may call a private telemetry helper;
telemetry cannot import storage, app or apphost, so no storage-to-app cycle forms.
Any package split needed to preserve this import graph is integrator-owned.

The existing callback receives only `*sql.Tx`; its captured request context remains
the server context. Thus transaction and outbound spans can be siblings under the
server span. Do not mutate a shared context's current parent or invent strict
transaction-to-client nesting. The required graph is App server→transaction and
App server→outbound client→private backend server, all with one trace ID. A
transaction's private child context stays inside WithTx; no new transaction API
is required.

`Transport(i)` returns only the owned HTTPS RoundTripper for a configured index;
invalid/unlisted destinations fail before any network operation. Clone the request
and header map. Require HTTPS, exact configured URL authority, matching/empty
Request.Host, no userinfo, fragment or opaque URL; caller data cannot select the
dial target. Strip every case variant/value of traceparent/tracestate/baggage, then
inject only the owned local context's canonical traceparent. Caller context/headers
cannot supply a parent when an owned scope is absent; omit propagation in that
case. Requests and caller headers remain unchanged. URL/path/query/body and
business authorization semantics otherwise belong to the application; tracing is
not the separate identity-delegating business proxy.

Each transport has a fresh private TLS1.3 configuration, copied CA and parsed
client certificate/key, normal authority/IP-SAN verification and exact pinned
literal IP dial. No DNS discovery, system roots, default proxy, environment proxy,
custom trust callback or caller RoundTripper. Require a current matching client
identity with explicit client-auth usage; require current normal verified server
identity. Disable automatic HTTP redirects/retries within this component; as a
RoundTripper it never follows redirects. A supplied http.Client must also reject
redirects; authority changes are denied by RoundTrip regardless. Finite connection
limits are4 total/idle per destination, idle timeout30s, TLS/header timeout2s and
whole outbound request timeout2s unless caller cancellation/deadline is earlier.
The timeout includes response-body use: wrap Close/EOF to cancel its owned context;
never buffer arbitrary response bodies for tracing. Do not claim streaming support.

## Authenticated collector and bounded failure behavior

Initial wire protocol is binary OTLP/HTTP over explicit TLS1.3 HTTP/1.1:
`POST /v1/traces`, `Content-Type: application/x-protobuf`, using the pinned
ExportTraceServiceRequest/ExportTraceServiceResponse protobuf messages. The
[official OTLP/HTTP specification](https://opentelemetry.io/docs/specs/otlp/#otlphttp)
permits HTTP/1.1 and this binary encoding. Do not change the pinned protobuf1.11.0
version or infer a protocol-version upgrade from the current specification.
Authority is a canonical lowercase TLS DNS name or canonical literal IP with an
explicit numeric port1–65535, maximum253 hostname bytes; no scheme, userinfo,
path, query, fragment, zone, ambiguous spelling or control/whitespace. DialIP is
one canonical literal unicast IP, never unspecified/multicast, and is bound to
this authority only. These same authority/dial rules apply to outbound destinations.
No default target or permission to deploy/provision follows from accepting config.

The collector connection is private, TLS minimum1.3, copied explicit CA roots,
normal configured-authority validation and an exact pinned-IP context dialer.
Server-authenticated TLS plus the dedicated bearer token authenticates this
collector path; no collector client certificate is required or accepted by this
initial config. The business outbound transport remains mutually authenticated.
Supply exactly `Authorization: Bearer <configured token>`, never application
credentials. No DNS, system trust, proxy, environment fallback, caller headers,
credential callback, global error handler or raw diagnostic log.

Use a fresh owned standard HTTP/1.1 transport/client per batch, never an
ambient-configured SDK exporter. Disable HTTP/2 negotiation, keep-alives, automatic
compression and redirects; do not install HTTP/2 alternate transports. Set explicit
HTTP/1.1-only ALPN, `DisableKeepAlives`, `DisableCompression`,8KiB maximum response
headers and a dialer admitting at most one TCP dial for the batch. Exactly one POST
invocation uses the fixed configured authority and `/v1/traces`, no query, fragment,
userinfo, caller-selected path or method. Use a finite body without GetBody,
idempotency/retry headers or application-level retry logic. No redirected request,
connection retry or second HTTP POST is permitted, including after ambiguous
transport failure or authentication/overload/server responses. The complete
per-batch dial/TLS/write/read deadline is at most2s. On every exit cancel the batch
context, close response body and close the transport's connections; no exporter
connection/reconnect activity survives into idle queue time.

Bound the actual serialized request and decoded response bodies to64KiB each,
using limit+one detection for responses. Compression is disabled: request body is
identity encoded and responses must have absent/identity Content-Encoding. Require
HTTP200, exactly the expected protobuf media type (normal MIME parsing without
unsupported parameters), and a valid bounded ExportTraceServiceResponse. Unknown
protobuf fields, malformed messages, partial-success presence or rejection fail the
whole batch; no status description, partial-success message, header metadata or
raw response is retained/logged. An empty protobuf body is a valid default complete
success response only when status/content type meet these rules. Count Exported
only for all records in a completely accepted batch acknowledgement. Ambiguous or
partial results are discarded with finite failure/drop counters, never retried;
there is no durable delivery or exactly-once claim. The private SDK still owns
spans; only the fresh finite OTLP projection is sent.

On span end, offer the immutable finite record to a queue of at most256 records
using an immediate nonblocking admission operation; never await exporter, worker,
mutex held across network I/O or queue capacity. One worker owns one batch of at
most16 records; flush on16 or after at most100ms from its first record. Bound each
record's serialized projection to4096 bytes and the complete request, including
fixed resource/scope envelope, to64KiB using actual protobuf size before send.
Overflow drops records/batch observably instead of allocating/growing unboundedly.
A full queue drops the new record. All counts use saturating uint64 arithmetic.

Each batch gets at most one export attempt with a maximum2s deadline, independent
of request lifetime. No retry queue, detached retry goroutine, indefinite retention
or application backpressure. Discard after success, timeout, cancellation, transport
failure, invalid response or collector rejection/partial rejection. Saturating
numeric stats expose attempted/exported/dropped/failed records and active/queued
counts; no last-error text, destination, request identifier or private labels.
Attempted counts ended observations presented for projection; Exported counts
records in fully accepted complete batch acknowledgements. Dropped counts discarded observation records, including
admission/projection/queue/close losses and records discarded after export failure;
Failed counts attempted records whose export was not accepted. These counters
intentionally overlap and do not form an accounting identity. Unsampled work is
not counted as a drop. Active/Queued are bounded live gauges. Stats do not perform
I/O. Queue, current batch and recording admission together
bound retained state; exporter failure cannot expand them.

Close is idempotent and stops admission immediately. First close establishes a
shared shutdown deadline of at most5s, shortened by caller deadline; concurrent
closes cannot extend it. Drain only within that budget, then cancel any export,
drop remaining records, close private connections and terminate the sole worker.
Return a generic bounded-close result; do not print exporter diagnostics. A caller
with an already cancelled context can force prompt cancellation. Owned outbound transports reject new RoundTrip calls after close, before dialing.
Late span ends are counted as drops and cannot restart the worker. Hosts do not close shared
tracers implicitly. Request handling never waits for Close or collector recovery.

## Required implementation proof and delegation

T10.5's original two telemetry paths alone cannot qualify the public composed
outcome. Integrator must explicitly delegate the necessary public observability
facade, App/runtime optional field and wrappers, storage transaction boundary
hooks, host forwarding and focused external-consumer tests. No root dependency
version, migration, generated initializer/devrunner, provider or deployment change.
Independent source review, guarded landing and fresh scoped checks remain required.

Required evidence is actual App HTTP→actual PostgreSQL transaction→separate
verified mutual-TLS backend→authenticated TLS HTTP/1.1 OTLP collector. Use owned local
fixtures, dedicated copied trust/client identity/token, standard verified server
TLS state, finite allowlisted peers and an actual Go TLS HTTP collector that
checks the dedicated authorization header and decodes raw binary protobuf POST
requests at the exact path. Assert explicit parent edges above under100% sampling, ratio0/partial sampling
and capacity limits, public forgery isolation, consistent inherited child decisions,
and trusted continuation with unsampled narrowing but no incoming escalation.
No handcrafted TLS state or in-process fake exporter qualifies these boundaries.

Exercise actual PG commit/rollback, original panic identity and cancellation on
both storage handles; RuntimeDB uses an actual required TLS PostgreSQL fixture with a separately
qualified restricted runtime role. Missing TLS/restricted-role fixture fails
visibly and blocks this source acceptance; a mock or the development trust fixture
cannot substitute. An isolated fresh fixture may be provided under existing
approved capacity after design landing, preserving all other test services. Exercise
outbound success/failure/cancellation and unknown destination denial before dial.
Assert HTTP/1.1, identity encoding, response message/header limits and no
post-attempt export connection/dial activity after the queue drains. Redirect,
retry-provoking status and ambiguous connection-failure fixtures must observe no
second HTTP POST. Reject malformed/unknown/partial-success protobuf, wrong media
type, compressed response and oversized bodies/headers without retaining details. Reject wrong CA, authority/IP SAN, client identity, collector authentication,
poisoned OTEL environment and caller proxy/propagation attempts. Mutate input
config after construction to prove copied isolation. Try public SDK span mutation,
events, links, bogus resource/scope data and credential/SQL/body/query/base64
canaries; recursively inspect ALL decoded collector resource/scope/span/attribute/
event/link/status/schema/unknown fields, not selected strings alone. Verify
application error/panic behavior and existing nil/default-off paths remain intact.

Stop/block the real collector under concurrent load. Assert maxactive256,
queue256, batch16/64KiB, one worker, increasing drops/failures, bounded application
completion and bounded close. Use deterministic fixture deadlines and report
actual observed bounds; do not certify general production latency. An isolated
compiled mutation replacing nonblocking enqueue with blocking enqueue must make
the overload/request-latency regression fail; restore exact source, rerun GREEN.
Required PG/TLS/collector absence is an explicit failure, never a skipped pass.
Local verified wire tests qualify only these fixtures; production credentials,
collector access/retention, network deployment, operator consent and E16 remain
separate unqualified gates.

## Explicit integrator source delegation

After independent design landing, the integrator delegates directly necessary
`internal/telemetry/tracing*.go`, `observability/tracing.go` and tests, minimal
`app/app.go`, `internal/runtime/runtime.go`, `apphost/local.go` optional composition
and focused tracing tests, and `storage/db.go`/`storage/runtime.go` transaction
hooks plus tracing tests. The existing logger/metrics behavior must stay intact;
minimal shared middleware changes, if needed, require final integrator review.
Public-consumer proof belongs in `tests/telemetryconsumer/tracing*.go`; root owns
its module bookkeeping and any necessary clarification to the existing correlation
file comment. All tests import public application/storage/observability interfaces
when proving the external consumer. No test-only authority injection qualifies
TLS/role/trace continuation. Module manifests/sums, amendments, task/registry
progress, fixture provisioning and integration evidence remain integrator-owned.
No dependency version changes or new migrations are delegated.
