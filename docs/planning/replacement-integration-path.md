# Continuity integration path

This sequence connects the existing components to a usable, independently
rehearsed application. It uses the current 1,592-task inventory; it adds no
acceptance, task rows, authority API or provider permission. The 1,501 retained
original tasks and full SaaS/billing/generator scope remain intact.

Current source: `1894539dcd24dd3d0de532f1a511a9ed067ba9e2`.
The [host admission matrix](../contracts/continuity-host.md#source-grounded-host-authority-preflight-unadopted)
is already source reviewed. Do not repeat that audit as the next deliverable.
The current implementation step is the independently adopted
[email confirmation correction](../contracts/email-confirmation-freshness.md).
The next contract step is the exact shared current-session/authority design below.

## Dependency-ordered delivery

| Step | Existing task ownership | Concrete exit artifact and dependency |
| --- | --- | --- |
| 1. Finish confirmation freshness | T3.6 | Implement the adopted v1.23 two-file slice; prove actual challenge/email/person waits, atomic rollback, live commit and both original/early-consume negatives. Independent source review, guarded merge and fresh landed checks precede completion. |
| 2. Freeze the shared authority interface | T-RPL-HOST-CONTRACT.2 with integrator-owned T2.8 seams and T3.3 session ownership | One exact contract/ADR for same-instance opaque session provenance, a non-renewing caller-transaction recheck returning the current principal, explicit supported writer graphs and transaction/error/lifetime semantics. Resolve producer freshness and lock compatibility from the existing source matrix; missing proof is a named source blocker, not permission to invent an application credential parser. Step 1 is one prerequisite, not this contract's complete acceptance. |
| 3. Implement and qualify that shared identity boundary | T2.8/T3.3; existing T3.5/T3.9/T3.15 owners for any specifically required producer changes | Implement only the independently adopted interface and exact necessary producer/writer corrections. Real revocation, epoch, expiry, downgrade, same-instance, cross-person rotation, bulk revocation and callback/deferred-lock schedules must support the selected profile. Preserve foreign ownership; no blanket edit grant follows from this table. |
| 4. Resolve workspace and resource authority in the same transaction | T-RPL-HOST-CONTRACT.2 and integrator-owned authority adapters | Freeze and qualify current personal workspace ownership plus finite resource-read requirements and their complete row/predicate protocol. Return refreshed selection and principal explicitly. Design can proceed alongside step 2; composed qualification depends on step 3. Reuse shared workspace ownership mechanisms, not an application copy of resolver SQL or configured-owner equality. Organization/membership/entitlement requirements remain explicit for profiles that admit them. |
| 5. Deliver authorized retrieval | T-RPL-SEARCH.1 through .6 | Compose qualified identity/workspace/resource authority and the existing repository `Sources`/`Source` in one caller transaction. Prove revoked/foreign/denied/unavailable/digest cases. No duplicate search SQL or model dependency. Exact independent review and landed evidence establish SEARCH.6 only after steps 3 and 4. |
| 6. Compose finite HTTP and the source screen | T-RPL-HOST-CONTRACT.2 through .6, then eligible T-RPL-WEB work | Adopt finite routes/config/request/output/time bounds, exact migration registry allocation and buffered completion. Reuse the pure source renderer for full/fragment views, perform final current checks after waits, and release source bytes/status only after successful commit. Actual HTTP/browser checks include denial, cancellation, failed commit and no-store headers. A source screen does not complete full WEB.6. |
| 7. Complete the baseline business host | T-RPL-WEB.1 through .6; T-RPL-HOST.1 through .6 | Complete property, case, checklist, procedure, activity and unsent-draft journeys with qualified mutation authority, including the shared executor wherever operations are registered. Reuse landed domain/repository/guide/view components. Respect WEB.1's SEARCH.6 dependency and HOST.1's WEB.6/HOST-CONTRACT.6 dependencies; do not bypass them with a partial screen receipt. |
| 8. Rehearse the actual journey | T-RPL-REHEARSAL.1 through .6, followed by full replacement gates | Native signup, explicit verification, sign-in, current workspace, source search/detail, case/checklist/procedure work, unsent draft, sign-out, restart/recovery and revoked/foreign/CSRF/dependency failures. Independent real composed evidence qualifies only its actual scope. Baseline rehearsal is not T-REPLACEMENT-ACCEPT or full product acceptance. |

## Concrete decisions for the next authority contract

The session package must keep private provenance bound to its exact service
instance and successful middleware admission. It must not accept a caller session
ID, raw cookie, configured person ID or policy assertion as a substitute. The
proposed non-renewing recheck returns a refreshed principal, including assurance
downgrade; an Allowed decision alone is insufficient. Its caller must retain one
trusted same-database transaction through workspace/resource checks and completion.
These are required design decisions, not new callable APIs.

Choose lock *strength* as well as order from the actual writer graph. PostgreSQL
16 documents that `FOR SHARE` blocks row changes but permits `FOR KEY SHARE`;
`FOR UPDATE` conflicts with both. Therefore the earlier proposed exclusive
person lock is not the only candidate for a read-only authority boundary.
A shared-person/shared-session profile is a candidate to evaluate against the
existing issuance foreign-key edge, not proof that rotation or any writer is
compatible. See the [official row-lock conflict table](https://www.postgresql.org/docs/16/explicit-locking.html#LOCKING-ROWS).

Before adoption, resolve the complete supported transaction graph, including
existing caller-held locks, cross-person old cookies, challenge/factor/contact
writes, foreign-key and unique-index waits, callbacks, bulk revocation and
workspace triggers. A leaf helper cannot repair earlier lock acquisition.
Do not reduce a revocation set, weaken current-state checks or call database
aborts a successful concurrency schedule. Prescribe actual supported-writer
controls and a mutation that detects the rejected protocol.

The finite personal read profile can be an intermediate deliverable, but must
not silently enable unqualified mutation, organization, provider or agent paths.
Any profile restriction must be explicit in its adopted host configuration and
tested at real routes. Existing full-product tasks and owners remain; a proposed
profile is not authority to narrow those requirements.

## Retained gates

Current release mapping remains separately owned by T16.1; the historical matrix
cannot qualify the current inventory. Source identity, useful UI, actual service,
independent review, provider qualification, deployment and rehearsal are distinct
records. No current provider mutation, spend, cutover or retirement is part of
this integration sequence. Continue the complete original plan after the bounded
journey instead of treating an intermediate component as mission completion.
