# Private continuity source HTTP adapter

Status: proposed finite B1 transport contract; different exact-head design review
must precede source. This extends the private read composition with unexported
HTTP methods only. It does not admit a public host, listener, migration registry,
identity constructor, business mutation, or complete writer graph.

## Custody and exact private surface

The integrator owns `examples/continuity/host/source_http.go` and its tests.
The only production entry is `func (c *readCore) sourceHandler() http.Handler`.
It captures the existing private core; no callback, session service, transaction,
pool, principal, repository or renderer can be supplied. Its protected inner
handler calls only `c.source` for retrieval. The exact retained native session
service supplies middleware admission. A public principal is never admission.
The implementation adds no exported constructor or route registration.

The route set is exactly GET and HEAD `/continuity/sources` and
`/continuity/sources/<canonical lowercase UUIDv7>`. Unknown paths return 404;
unsupported methods on recognized paths return 405 with `Allow: GET, HEAD`.
There is no OPTIONS disclosure, download, mutation or effect. HEAD performs the
same current-authority and committed retrieval as GET but sends no body. The
first profile always selects the current personal workspace; it does not accept
a workspace query selector that would be lost by existing renderer links.

## Input bounds and deterministic parsing

Apply the original caller context with an upper bound of five seconds before
middleware admission; every child operation inherits the remaining duration.
Never replace it with a background context or extend it at a later phase.
Reject canceled requests without database work. This duration bounds admitted
work, not network receipt; a later listener must independently bound headers,
body reads, writes, idle connections and concurrency.

Before authentication or retrieval, reject paths over128 bytes, raw query over
2048 bytes, nonempty RawPath, noncanonical detail identifiers, encoded path aliases,
userinfo/absolute-form URL authorities and URL fragments. No path normalization,
redirect, double decoding or fallback route is performed. Request bodies are not
accepted: positive or unknown ContentLength and any TransferEncoding return400.
The adapter does not read a body. The eventual listener owns request disposal.

Use `url.ParseQuery` and reject malformed encodings, unknown keys and duplicate
values. List keys are only `q`, `kind`, `after`, `limit`. Detail permits no query.
Empty/absent q, kind and after retain existing renderer/repository semantics;
limit defaults to25 when absent, otherwise canonical decimal1..100 only. Validate
all selectors to the existing source-view bounds before authentication: canonical
UUIDv7 after, q at most480 bytes and120 code points, valid UTF-8, controls rejected
before trimming, and finite kind. Return400 with fixed text for invalid input.
Never echo supplied values into an error, header or redirect.

Fragment mode is selected only by a single `HX-Request: true` with a single
`HX-Target: continuity-source-content`. With no HX-Request, require no HX-Target
and return a full document. Other, missing or duplicate fragment metadata falls
back to the full document under the existing UI contract. These headers
select presentation only and never bypass middleware or current authority.
Existing renderer links and ordinary GET forms remain fully functional without
JavaScript. This adapter does not load HTMX or promise enhanced navigation.

## Completion, response and error boundary

Wrap the entire native middleware and inner handler in a private bounded response
collector. It implements only `http.ResponseWriter`; no Flush, Hijack, Push,
ReaderFrom or unwrap escape exists. It stores at most512 KiB of body and8 KiB of
header names/values, with at most32 values. Overflow marks failure, discards the
buffer and prevents later writes from publishing. Informational status codes,
redirects, cookies, unknown status and unexpected headers are rejected at final
validation; no provisional middleware output is copied to the real writer.

The inner handler obtains its byte slice only after `c.source` has returned from
successful transaction completion. It maps fixed internal error classes:

| Outcome | HTTP status and body |
| --- | --- |
| Committed authorized retrieval | 200, the existing bounded full/fragment source renderer bytes |
| Invalid selector | 400, fixed invalid-request text |
| Established denial, missing or foreign detail | 404, fixed non-enumerating Source not found message |
| Unavailable database, policy, renderer, transaction or completion | 503, fixed Sources are temporarily unavailable message |
| Native authentication denial | 401, fixed authentication-required text |

Native middleware failure status401/403/503 is preserved but its body/headers
are replaced with the adapter's fixed response before final header validation.
Collector overflow still fails unavailable regardless of the earlier status. Unexpected middleware results,
panic, collector failure, empty success or cancellation before publication become
503 with no provisional source bytes. A defer recovers only inside this private
HTTP boundary; it cannot claim a committed renewal or read was rolled back.
No automatic retry occurs. The final cancellation check occurs before copying
headers/status/body to the actual writer. Network writes can fail or partially
complete after publication starts; this component promises no rollback of bytes
already delivered and performs no database or application work after that point.

Every response sets `Cache-Control: no-store`, `Pragma: no-cache`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
`X-Frame-Options: DENY`, and `Vary: HX-Request, HX-Target`.
All responses use `text/html; charset=utf-8`. Errors use a fixed private
html/template with the same source section ID and full/fragment document shape,
a fixed back link and escaped code/message/request_id fields. Generate one fresh
server request ID for each request, never reuse the incoming header; set the
same X-Request-ID response header and error field. This preserves the frozen
error contract while replacing native middleware diagnostics. Use an error-returning
UUID generator; entropy failure returns a fixed503/no protected bytes and no
fabricated identifier. Success rendering still uses the existing sourceview
functions unchanged. Content-Length is the selected body byte length for
both GET and HEAD; HEAD omits the bytes on every outcome. Omit ETag,
Last-Modified, cookies, Location and user-supplied correlation headers. CSP is
`default-src 'none'; style-src 'self' 'unsafe-inline'; base-uri 'none';
form-action 'self'; frame-ancestors 'none'`: fixed existing renderer inline styles
remain usable; scripts, remote resources and unsafe content conversions are absent.

## Independent evidence and remaining admission

Pure tests cover exact routes/methods, every input bound/duplicate/alias,
full/fragment selection, headers and HEAD, collector overflow, panic and canceled
publication using test-only handlers. They are transport-mechanics evidence only.
Actual native middleware and real committed source retrieval must separately prove
200 list/detail/literal search, missing credentials, denied/foreign resources,
unavailable dependencies and zero disclosure on transaction failure. Those tests
require independently reviewed fixture source and a fresh serial operator grant.
No source author receives a fixture configuration or launches a database.

Different review must trace custody and prove no provisional output reaches the
real writer, including middleware failures. Meaningful isolated negatives must
fail the relevant assertion and exact restoration must pass. Any separately restricted verification remains excluded and unqualified; this
transport contract does not broaden runtime verification permissions.

Complete W1 participants, operation policy/executor, native signup-to-business
journey, public configuration/listener, immutable installed migration history,
RB1-RB10 and full provider/product/release gates remain open. This private adapter
is not a public-host workaround, a completed task or permission to merge PR77.
