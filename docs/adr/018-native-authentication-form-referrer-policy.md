# ADR 018: native authentication form referrer policy

Status: accepted implementation boundary; browser qualification recorded separately.

Default authentication pages use `strict-origin` in both the HTTP header and
HTML metadata. This removes the path and query, including verification and
reset proofs, from outgoing referrers. Application actions continue to require
an exact configured Origin; `null` and absent origins remain rejected.

The [Fetch Standard's request Origin algorithm](https://fetch.spec.whatwg.org/#append-a-request-origin-header)
sets the Origin of native non-CORS form submissions to `null` under
`no-referrer`. A real generated-application browser run exposed the resulting
signup rejection. `strict-origin` preserves native same-origin submissions
while omitting referrer paths and queries, and suppresses referrers on secure
transport downgrades. Public origin disclosure is accepted for these pages.

Scanner GET and HEAD remain non-consuming. Challenge forms contain no external
resources, responses remain non-cacheable, and same-origin/CSRF/rate-limit
checks are independent of referrer policy. This does not qualify live email,
cloud deployment, or the entire authentication suite.
