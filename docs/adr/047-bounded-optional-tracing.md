# ADR047: bounded optional tracing with private recording state

Status: proposed; independent design landing precedes implementation.

Adopt [bounded tracing v1.33](../contracts/bounded-tracing.md) for T10.5. A nil or
default-disabled tracer has no exporter, worker or network effect. Explicit owner
configuration selects local sampling and finite authenticated destinations; it
never changes application identity, policy, readiness or existing log correlation.
Use the pinned private SDK and owned bounded queue/worker, avoiding ambient/global
instrumentation and environment-selected destinations.

Business contexts expose only nonrecording SpanContext plus opaque private state.
Fixed names/ordinal outcomes, SDK/active limits and a second fresh OTLP projection
exclude arbitrary SDK metadata and raw application data. Public ingress starts a
fresh trace; continuation requires actual verified mTLS plus copied SPKI allowlist.
A private root sampling decision is inherited by all local children. Trusted
unsampled parents may narrow it; incoming sampled bits cannot increase it. The outbound transport
uses copied explicit trust/client identity and pinned-IP authority binding.

The worker sends pinned binary OTLP protobuf by explicit TLS1.3 HTTP/1.1
`POST /v1/traces`, with a dedicated bearer header. A fresh one-dial transport per
batch disables keep-alive, HTTP/2, compression, redirects and retries; request and
response sizes and the complete attempt deadline are bounded. Close every attempt
rather than retaining idle reconnect state. Only a complete200/protobuf success
acknowledgement counts as exported; ambiguous/partial responses are dropped, with
no durable or exactly-once claim. The business outbound channel retains mTLS.
This uses existing pinned protobuf1.11.0 and no ambient SDK exporter or dependency
upgrade. An actual authenticated TLS HTTP collector fixture proves wire behavior;
collector operation and production retention/deployment remain separate. Immediate
queue admission, one-attempt bounded export and bounded shutdown make exporter
outage a numeric diagnostic failure instead of an application dependency. These
bounds intentionally trade trace completeness for application availability.

Preserve WithTx signatures and original error/panic semantics. Server-to-database
and server-to-outbound spans are siblings when a callback retains its request
context; backend-server parent is the outbound client. Do not invent a mutable
current parent or claim a linear chain that the existing API cannot represent.
Host composition forwards caller-owned tracers and never closes shared ownership.

The two original telemetry task paths need explicit integrator expansion to the
public facade/runtime/storage/host and actual cross-service tests before dispatch.
No dependency upgrade, production constructor, provider, destination or resource
allocation is selected by this decision. Source acceptance requires actual PG,
verified private mTLS and authenticated decoded OTLP wire plus compiled
blocking-enqueue RED/exact-restoration GREEN; design review alone proves none of
those behaviors. Trace IDs/timing/topology retain fingerprinting risk; this design
makes no anonymity claim.
