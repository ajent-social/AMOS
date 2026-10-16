# Bounded optional tracing qualification

T10.5, contract v1.33. This record covers the explicitly enabled component and
local integration tests. Collector deployment, retention/access policy, production
mTLS, external providers, hosted CI and release admission remain unqualified.

The owner composes a caller-owned `observability.Tracer` through `app.Options` or
`apphost.LocalConfig`; nil/default-off performs no tracing network activity. Hosts
never close a shared tracer. Enabled configuration selects copied trust, a pinned
literal dial address, exact authority, dedicated collector authentication, sampling
and finite route/destination ordinals. No global SDK/provider/propagator is changed.

The private SDK and a second protobuf projection retain only the documented finite
span vocabulary. Application contexts expose nonrecording span contexts; supplied
SDK spans, propagation headers and baggage do not establish observation authority.
Trusted continuation requires the actual verified mTLS peer and a configured SPKI.
This continuity never grants application identity, workspace or billing authority.

Actual local integration exercises App HTTP → PostgreSQL transaction and App HTTP
→ owned outbound client → mTLS backend App → authenticated binary OTLP/HTTP
collector. Transaction and outbound spans are siblings; callback signatures remain
unchanged. Both development DB and TLS/restricted-role RuntimeDB paths are checked.
The fresh restricted fixture verifies TLS1.3 with normal hostname/CA checks, real
DML, privilege denials, wrong-password authentication denial and plaintext denial.
It does not qualify an external database provider or production installation.

Independent preliminary review found abandoned-body admission leakage, failed-root
context leakage, propagation into a nil header map, and invalid broadcast address
admission. The candidate adds deadline-owned body cleanup, inert contexts on failed
admission, private cloned headers and unicast validation with regression tests.
Tracing also uses a private status-only response wrapper so it does not buffer
application error bodies or change correlation headers. A successful rollback is
an observation of completed cleanup, not business success; callback errors remain
unchanged and failed rollback is an error outcome.

## Source checks

On the qualified Linux test host, Go1.27.1 race checks passed257 named tests and
subtests, zero failures and zero skips across these bounded selections:

| Selection | Named passes |
| --- | ---: |
| Full internal telemetry and public observability | 71 |
| Full App, runtime and storage | 175 |
| Focused host caller-owned tracer check | 1 |
| Full external telemetry consumer module | 10 |

The71 core checks include49 tracing and22 existing checks. Both source groups
passed scoped vet and pinned lint with zero issues. These are local checks, not
hosted CI. A complete apphost/browser or whole-repository suite is not claimed.

The required compiled mutation replaced nonblocking enqueue with blocking enqueue:
RED detected request backpressure; exact restoration passed the targeted race
check. Restored export-source SHA256 is
`a8efee361c5a9800686cf228ae98b879239c9443152388a7a1df035950c3e388`.
A separate compiled mutation removed the storage observation hook: the actual
collector graph check failed for missing transaction spans. Exact restoration
passed the full integration selection; restored storage-source SHA256 is
`cd7723212264cb71e4bfab13c03a97cc8d9817bfd891e68e2735b302c6b8b84a`.

All changed/new public paths pass the unchanged artifact scanner. A whole-checkout
scan remains red with61 findings exactly identical to an isolated baseline at
`780d91b1f47cbf89fc22482a2f21fbfd63a058bf` (same paths, lines and categories).
[Follow-up135](https://github.com/ajent-social/AMOS/issues/135) tracks individual
reconciliation; no scanner weakening or blanket suppression was applied.
Bounded plan semantics and in-memory projection generation pass; this does not
qualify native workflow admission or release execution.

## Landing status

Final complete-candidate independent review, guarded merge and fresh landed
verification remain pending. This draft does not accept the task.
