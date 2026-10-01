# Planning review and resolutions

Date: 2026 09 30 UTC. Review scope: architecture and task plan, not product code correctness. Three Astra specialists drafted bounded domains; separate architecture/security passes checked the assembled design. The integrator performed dependency, coverage, ownership-wave and publication checks.

| Finding | Resolution |
|---|---|
| Missing S1 shared operation policy runner could cause duplicate engines or dependency cycles | T2.8 owns policy/idempotency/transaction invocation; T5.9/T12.3/T11.4 consume it |
| Baseline real-provider gate required subscription management that only existed in later stage | T5.22 and T6.18 add personal flat-subscription management; T16.3 qualifies only baseline identity/flat lifecycle; full methods remain later |
| S1 fixtures accidentally required organization lifecycle | T7.6 uses a personal workspace; production never links fixture transports |
| Job fencing overclaimed prevention of remote side effects | T1.10 guarantees fenced local completion; remote effects require provider idempotency and reconciliation |
| PostgreSQL wording lagged confirmed two-profile choice | VISION/RFC/ADR 008 now distinguish selected PostgreSQL profiles from still-proposed concrete adapters |
| Export URLs could bypass current-owner revocation | T1.13 requires authenticated download boundary for immediate revocation; presigned-link exceptions require explicit bounded semantics |
| Older independent maintenance approval could overwrite newer release | T13.10 binds source/artifact/target generation, rechecks current policy/pause and compares-and-swaps installed state |
| Restored epochs, sessions or decisions could resurrect authority | T3.23 uses non-restored activation authority or owner rotation; T10.11 depends on this barrier; T3.23 consumes isolated restore harness, avoiding a cycle |
| Proxy and extension tasks might create competing authorities | T12.3 delegates to T2.8; T12.6 consumes T2.9 signer/verifier |
| Invalid diagnostics quarantine might retain raw prohibited payloads | T14.4 discards prohibited raw content and retains only sanitized rejection metadata |
| Upstream intake lacked independent deployment contract | T14.13/T14.14 specify and qualify separately owned service infrastructure |
| Final release could omit branches such as secondary profile and upgrade compatibility | T16.12 depends on all remaining terminal branches; early staged releases retain narrower advertised capability gates |

## What checks prove

The planning validator checks task/use-case uniqueness, resolved dependencies and ordering, contract sections, complete use-case coverage, wave capacity/ownership and document links. It cannot prove that estimates are accurate, commands will pass, libraries are compatible, an inexpensive agent will execute a contract successfully, or any provider/cloud feature works.

All task certification remains NOT_RUN. Exact-version provider/security qualification, independent acceptance and owner-approved live checks remain implementation work. Source inspection and this review are not a security audit or a production readiness verdict.

## Executed planning checks

The canonical plan passes structural validation. Four isolated malformed-plan fixtures were rejected as expected: missing dependency, self-cycle, later-stage dependency and uncovered use case. These are checks of the plan validator, not application tests. All verification-script paths referenced by contracts have a planned owner or already exist as planning utilities.


## RFC 0002 document review (2026-10-01)

The owner requested resolution of the lifecycle gaps and exclusion of closed-source project names from public AMOS artifacts. [RFC 0002](../rfc/rfc-0002.md) now specifies dedicated machine-job authority and revocation, scoped/fenced scan reconciliation with evidence-based deletion, collection and destination-specific disclosure allowlists, and feature evolution with compatibility, migration and recovery rules. Its acceptance table and adoption gates include corresponding negative cases.

External runtime references in both RFCs, the vision, handoff and supporting planning notes use neutral roles. Public integration requirements must be qualified from supported public contracts and actual controller checks, not private source or endpoint registries. Repository instructions explicitly exclude closed-source project names. This is a draft documentation revision; frozen shared contracts, task acceptance, deployment and provider status are unchanged.

Verification obtained: the planning validator passed (16 epics, 257 tasks, 116 covered use cases, 48 waves); the public-artifact checker and whitespace check passed. RFC/review-record relative links resolve. Both reference images were inspected; the shell mock's private integration labels were replaced with neutral wording and the revised image inspected. A case-insensitive scan of Git-visible text found no remaining occurrences of the removed private runtime name. These are documentation/publication checks only.
