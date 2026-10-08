# Standalone continuity application acceptance

Status: proposed mapping; no replacement acceptance is asserted. The authored
inventory is `wazi-source.json`; all original 1,501 rows and evidence remain.
This bounded journey supplements the complete SaaS and operating plan. It does
not redefine full-platform acceptance or require universal third-party parity.

The first independent application is a continuity desk with application-owned
data and workflows. It uses Go, server-rendered HTML, HTMX and ordinary CSS/JS.
The useful local baseline requires no paid account, external agent or hosted
control plane. Optional integrations expose their absence visibly. Existing
local host qualification is reusable evidence for its exact existing boundary,
not proof of a newly composed application.

| ID | Observable journey and negative cases | Delivery slice | Current evidence |
|---|---|---|---|
| RB-1 | Browse a synthetic property register, search bounded fields, open details and linked sources; unknown and foreign records disclose nothing. | RPL-DATA, RPL-WEB | NOT_RUN |
| RB-2 | Change maintenance status and refresh/restart; reject unknown states and stale revisions; status is an operator record, never proof of approval, booking or completion. | RPL-DATA, RPL-WEB | NOT_RUN |
| RB-3 | Save and reopen an unsent owner-update draft tied to the current case revision and source references; no send endpoint or effect intent exists. | RPL-DATA, RPL-WEB | NOT_RUN |
| RB-4 | Update supporting application checklist items; final human decision remains disabled at both service and HTTP boundaries; no scoring/ranking/selection. | RPL-DATA, RPL-WEB | NOT_RUN |
| RB-5 | Edit a procedure with revision conflict detection and atomic activity recording; refresh retains it; procedure text is data and cannot alter executable rules. | RPL-DATA, RPL-WEB | NOT_RUN |
| RB-6 | Open source correspondence and documents, search local text with bounded results and stable content digests; unavailable optional retrieval remains visibly unavailable. | RPL-SEARCH, RPL-WEB | NOT_RUN |
| RB-7 | Guided deterministic briefing, case explanation and handover use current stored state and resolvable source references; unknown authority stays unknown and unsupported questions are explicit. | RPL-GUIDE, RPL-WEB | NOT_RUN |
| RB-8 | Signup, verification, sign-in, personal scope, protected reads/writes, sign-out and revoked/foreign/CSRF denial through the composed app; DB outage cannot report success. | RPL-HOST, RPL-REHEARSAL | Existing local host component only; new composition NOT_RUN |
| RB-9 | Keyboard, narrow-screen, empty/error/conflict states and escaped untrusted records; ordinary forms work without JavaScript. | RPL-WEB, RPL-REHEARSAL | NOT_RUN |
| RB-10 | Fresh isolated install, additive migrations, restart persistence, backup/restore rehearsal and exact source/artifact receipts; existing installations stay untouched. | RPL-HOST, RPL-REHEARSAL | NOT_RUN |

New synthetic records must be independently authored. No private source,
identifiers, design assets, endpoints or data are public implementation inputs.
Application definitions, data rules, migrations and scenarios belong under
`examples/continuity/`; shared tools accept domain-neutral contracts. This is a
reference app, not hardcoded production answers in framework packages.

## Scope and qualification

Source, isolated rehearsal, provider qualification, deployment and actual cutover
are separate states. The acceptance rows above require real PostgreSQL and a
real browser for the composed journey; mocks do not qualify persistence or auth.
The baseline includes no bookings, money movement, external email sending,
automated tenancy decision or live external account. Billing, selected identity
methods, API/MCP and both deployment profiles remain in the original full plan.
They become prerequisites when those capabilities are advertised, not merely
because this no-effect local baseline exists.

A temporary integration bridge is installation-specific and retires only after
its consumer journey, authority, retained data and reconciliation gates pass.
There is at most one effect dispatcher for any effect scope and zero is valid
while quarantined. Uncertain outcomes require reconciliation or forward repair;
retry is never inferred from a timeout. This batch authorizes source and isolated
rehearsal only. No cutover, data destruction or production shutdown follows.

## Missing lifecycle inventory

RFC 0002's feature/view/operation schema remains proposed. Explicit graph rows
cover contract adoption, source, verification, independent review, guarded merge
and landed checks for this baseline, plus later generic feature definitions,
view/operation binding, feature evolution and optional host context. Each source
slice waits for its exact adopted contract. Review/fix/re-review is mandatory;
a review-stage label or narrative COMPLETE cannot satisfy the gate.

Existing task and release registries remain the evidence authorities. The new
rows start PLANNED and the portable exporter retains pending authored statuses.
The plan-tool recovery source slice is separate from application implementation.
