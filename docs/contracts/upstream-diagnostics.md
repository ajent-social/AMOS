# Public diagnostic profile v1

Status: proposed T14.1 design contract for independent review and integrator
freeze. This document and the schema/checker establish a finite proposed wire
allowlist. They do not implement event sanitization, owner consent, local spool,
transmission, collection, deletion, or any production privacy control. T14.2 and
T14.3 own construction and export. No automatic transmission is authorized.

## Finite public registry

The wire profile is `amos-public-diagnostic-v1`, registry
`amos-public-diagnostic-registry-v1`. Component approval is an explicit reviewed
registry entry, never a caller-provided package name or semantic-version string.
This initial registry contains only `amos.http-observability`, public component
baseline `amos.http-observability.v1`, with manifest SHA-256
`40020920667a6ff56650b4d5e792356ca6f522b3a0e5c729f68074a6053dbdd3`. This is an exact
component-code baseline, **not** the whole installed repository commit, release
version, dependency closure, artifact digest or maturity claim. Its public source
was inspected at `0208b87c7bc24d058d327a08d5634c6ef77af4ee`; that commit is an
audit locator, not an installed-release admission condition.

The manifest is the following sorted UTF-8 text: each line is a repository-relative
path, one space, the lowercase SHA-256 of exact file bytes, then one LF newline.
Its own schema, registry/checker, documentation and tests are excluded, so adding
or reviewing this registry cannot change its component fingerprint.

```text
app/app.go 37d292c2fe6578c1030447e4b948fd34b162c622ace3cf53b9545de40bc08a77
internal/health/handler.go 87b0a138f70e12ba67026ab5150d48083fb6027e6346b067c90c7d45b8d63e0b
internal/runtime/runtime.go c104667bad38fe1dd8ee8e6da0927837152935f5a7fc8d2f40ad38012e31f53f
internal/telemetry/logging.go c1adf2275d6ca681c958c8a39be9ff5353e70292330d7a693275444922480f67
```

A future exporter must establish, through independently qualified trusted build
provenance, that the executing component corresponds to these exact source bytes
before selecting this registry entry. A new full application commit containing
unchanged component files can truthfully report this component baseline; it must
not relabel its entire installed build as the old audit commit. Caller labels,
self-asserted payload fields, a matching symbol name, or arbitrary semver are not
provenance. Unknown/modified component code stays local until a reviewed new
registry baseline admits it. Runtime provenance verification is future work and
is not established by the schema decoder. The source-manifest test checks the immutable approved manifest definition and
digest only. It does not require current repository files to match that old
baseline: new source may exist while diagnostic export for the changed component
remains unsupported. A different current file hash is not permission to change
the frozen baseline, auto-admit the new component or relabel it as approved.
Actual source comparisons are explicit qualification evidence; installed-binary
provenance remains separately required. No AMSL revision or frame is approved by
this profile.

For source qualification only, run in the exact candidate checkout:

```sh
sha256sum app/app.go internal/health/handler.go internal/runtime/runtime.go internal/telemetry/logging.go
```

Compare each result with the immutable manifest above. Record the source revision and whether
it matches or is unsupported. This command is not a persistent repository test,
a runtime provenance verifier or proof of an installed binary. A later source
mismatch is expected until a separate registry review admits that component.

| Field | Exact approved vocabulary |
| --- | --- |
| Aggregate window | `24h`; no precise timestamp, installation/actor/workspace ID or request ID |
| Metric names | `http.requests`, `http.server_errors`, `readiness.failures` |
| Count buckets | `0`, `1`, `2-4`, `5-15`, `16-63`, `64+` |
| Error/code signature pair | `dependency.unavailable` / `http.required_dependency_unavailable.v1` |
| Error/code signature pair | `handler.panic` / `http.handler_panic.v1` |
| Public frame ID | `amos.health.serve_http.v1` |

The sole frame ID maps to exact public symbol
`github.com/ajent-social/amos/internal/health.(*Handler).ServeHTTP`, declared in
`internal/health/handler.go` in that exact approved component baseline. It carries no
source filename, line, argument, address, URL or raw symbol on the wire. A future
sanitizer must match exact reviewed source provenance and symbol identity to this
registry before constructing the ID. Package-prefix matching, matching a suffix,
regex removal of private substrings, and replacing an unrecognized frame with a
public-looking placeholder are forbidden. A frame containing a business symbol,
home/build path, arguments or any other unapproved material is dropped **whole**;
no part is copied into an exported frame. Raw unknown frames never reach this
wire decoder. If nothing remains, `frames` is an empty array.

Signatures are fixed registry identifiers mapped from typed public event codes;
they are not hashes of raw messages, stack text, URLs or identifiers. Unknown
components, versions, metrics, codes, signatures and frames stay local. Error
messages, raw logs/source, arbitrary metadata/string maps, deployment/host/account
identifiers, business data and nested unknown properties are ineligible. The
schema closes every object; no extensions bag or freeform string is defined.

## Privacy budget and owner control

Before any downstream sender exists, reporting defaults **off**. Explicit owner
opt-in is separate from application startup. Owners can inspect the exact
serialized bytes and destination before a manual export, disable immediately,
and lower retention/rate/count limits including zero. An unavailable collector,
full spool, invalid payload or declined consent cannot affect application service
or trigger fallback transmission. Disabling cancels unsent work and deletes the
pending local diagnostic spool; it does not promise recall of already received
data. A future collector must implement a separately reviewed deletion policy
before collection is enabled; received payloads have the same hard seven-day
retention ceiling, with owner-requested earlier deletion. Transport/access logs
require a separately reviewed bounded policy before enabling collection and may
not retain payloads or extend payload retention. No collector is qualified here.

The hard profile ceilings are 8,192 UTF-8 payload bytes, one component, three
metric aggregates, two error aggregates, and one public frame per error. There
may be at most one report attempt per rolling 24 hours, including retries and
failed sends, four queued reports and seven days (168 hours) local retention.
Enforce rate state across process restarts, use a monotonic elapsed-time guard,
and deny sending on unknown/reset/tampered budget state. No catch-up burst or
new report identity may reset a consumed budget. Never resend an expired report.
Owner-selected lower limits cannot be increased by a diagnostic payload.

These are normative downstream requirements, not controls implemented by this
design package. The byte ceiling applies before JSON decoding, including
whitespace/encoding overhead. It cannot be enforced by JSON Schema alone.
Metric names and error codes appear once each in lexicographic order, frame IDs
are unique, and no alternate ordering is used as extra metadata. Payloads contain
bucketed aggregate counts only; do not encode identifiers as counts, signatures,
registry choices or report timing. Runtime collection must apply the declared
aggregate window and thresholds; arbitrary caller aggregates are not evidence.

## Checks, evolution and remaining qualification

`api/schemas/upstream-diagnostic.schema.json` is JSON Schema Draft 2020-12.
`internal/diagnosticcontract.DecodeAndValidate` strictly checks this exact finite
profile, rejects duplicate/unknown JSON keys using exact decoded, case-sensitive property
names at every object boundary before Go typed decoding, enforces byte/count/registry limits
and canonical uniqueness/order, and returns a generic rejection without echoing
input. It is **not** a general JSON Schema validator. Meta-validation and fixture
validation through an independent standards implementation remain separate checks;
Go structure checks do not substitute for them. Standard schema `uniqueItems`
compares complete objects: it does not reject a repeated metric name with a
different bucket, nor enforce lexicographic order. Those key-uniqueness/order
checks, duplicate JSON-key rejection and the byte ceiling are additional mandatory
finite-profile checks. A schema-valid but noncanonical payload is rejected by
`DecodeAndValidate`; downstream senders must apply both layers, not schema alone.
Fixtures explicitly exercise this distinction. Schema validation cannot prove
consent, truthful aggregate provenance, source approval, whole-frame omission,
rate/retention enforcement, or safe transport.

The rejected application-frame/home-path fixture must fail as an entire input.
Its expected exported design fixture contains an empty frame array, not a
redacted frame. This tests the agreed document boundary; it does not demonstrate
an implemented sanitizer. T14.2 must implement a fresh typed projection and prove
actual whole-frame dropping at the construction boundary.

Wire or privacy-budget changes require independent review, an integrator-owned
frozen-contract amendment and a new compatible profile/registry version with
migration notes. Added source snapshots, symbols, metrics or signatures require
explicit approval; a permissive semver matcher or caller label cannot admit them.
Old unsupported profiles fail closed. No remote registry fetch, dynamic approval,
network permission, owner approval inference or reporting activation is added.

Even this small vocabulary can fingerprint rare versions/error combinations and
expose activity buckets. IP addresses, transport timing and collector access logs
can identify an installation independently of payload fields. This is no
anonymity guarantee, differential-privacy claim or permission to send personal
data. Review transport/linkability and owner-visible privacy disclosures before
any collector or live export is qualified.


## Proposed v1.37 collector clarification

The [intake admission contract](upstream-intake.md) consumes this finite wire
allowlist without accepting source/build-provenance claims. A recognized wire
baseline remains unverified quarantine data. Invalid input is discarded without
persisting or hashing its raw body; no instruction, URL or arbitrary signature is
rewritten into an approved value. Only freshly normalized validated finite payload
bytes may receive a canonical digest. Source/build provenance for later export or
trusted analysis remains separate. No collector, consent, transmission, enrollment
or public-release authority follows from adopting this admission design.
