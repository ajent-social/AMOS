# Agent access, maintenance and security planning input

This input proposes E11-E15: **66 tasks, 40 use cases (UC-080-UC-119), and 85 estimated coding/design hours** before integration, external review, provider setup or live qualification. All use cases are PLANNED. The companion [machine-readable inventory](../inputs/agents-maintenance.json) contains task instructions, owned paths, dependencies, negative tests and scoped future verification commands. It is a complete future inventory with stage-entry revalidation, not execution-certified work.

The intended behavior comes from [VISION](../../VISION.md) and [RFC 0001](../../rfc/rfc-0001.md). Standard external MCP clients receive ordinary application authorization, tenant isolation and entitlement enforcement. AMOS does not host customer agents or external agent runtimes, own their governance inbox, or invent a future agent protocol. Sensitive operations use ordinary resumable identity proof rather than an agent bypass.

## Use case and task summary

| Epic | Tasks | Use cases | Primary lane | Stage intent |
| --- | ---: | --- | --- | --- |
| E11  -  REST/OpenAPI-to-MCP access and credentials | 14 | UC-080-087 | L14 | Full API/MCP qualification, S3 |
| E12  -  Business extensions and custom clients | 12 | UC-088-095 | L14 | Local integrated seam S1; network contract S2; full proxy/custom client S3; compatibility S4 |
| E13  -  Private owner maintenance | 13 | UC-096-103 | L15 | Owner-controlled autonomy, S5 |
| E14  -  Sanitized diagnostics and upstream repair | 12 | UC-104-111 | L16 | AMSL gap packet available S1; privacy/update contracts S4; upstream loop S5 |
| E15  -  Security and recovery qualification | 15 | UC-112-119 | L16 | Threat model S1; ingress/build contract S2; interface security S3; supply chain/recovery S4; autonomy attacks S5 |

UC-087, UC-094, UC-095, UC-102, UC-110, UC-111 and UC-119 are P1; the other 33 are P0. None is implemented by this planning work. Interface paths and package paths are proposals to freeze under T1.2, not claims about existing routes or files.

## Public-source evidence and qualification limits

Only public AMSL mechanisms and publishable repository-relative descriptions are used here. No private implementation, customer identifier, account identifier, infrastructure address or private adoption evidence is reproduced.

1. **`servicecred` is a mechanism, not application authorization.** The inspected public `AMSL go/servicecred/servicecred.go` defines exact case-sensitive owner/resource/scope bindings. `Issue` narrows a caller-authorized ceiling, enforces expiry and returns a secret after durable create. `List` returns sanitized metadata. `Verify` reads authoritative current credential state and explicitly requires application-owned status/authorization checks. `Revoke` takes a caller-authorized exact binding. Lost issue responses require revoke/reissue; admitted work may finish after revocation. T11.5-T11.7 compose these guarantees with current workspace roles, entitlements and UI. The public catalog still marks the mechanism CANDIDATE.

2. **`mcpoauth` has a declared narrow subset.** The package contract in public `AMSL go/mcpoauth/mcpoauth.go` states one fixed issuer/resource and public PKCE clients. Login, browser CSRF, consent and live ownership checks are the application's responsibility. Grant creation and refresh rotation are atomic store contracts. Refresh reuse revokes the family; lost refresh responses are not made safe by inventing an undocumented grace period. The public candidate catalog explicitly excludes confidential clients and remote metadata retrieval from the stated subset. T11.1 freezes the actual supported profile; T11.8-T11.11 qualify its composition.

3. **SQL storage does not justify assumed multi-issuer isolation.** The inspected `AMSL go/mcpoauth/sqlstore/store.go` uses global ID-keyed lookups for clients and other records; resource columns exist on relevant records but are not by themselves proof that every object is isolated between issuers. This is a qualification risk, not a claimed exploit. T11.8 requires two synthetic issuers, colliding identifiers, cross-consent/code/grant/refresh attempts and actual database ordering tests. The supported strategy may be an isolated schema/store per issuer or a reviewed narrower adapter. No sibling change is authorized by this plan.

4. **Existing upstream planning already names the seams.** Public `AMSL go/docs/identity-composition-plan.md` IC01-IC05 covers issuer isolation, transaction-time live policy, user-managed service credentials, lifecycle qualification and multi-host/restore responsibilities. These tasks reuse that work instead of assuming the libraries provide a complete account framework. A missing generic guarantee requires a failing publishable consumer case, alternatives review and scoped contribution. Product policy remains in AMOS.

5. **Do not implement another MCP stack.** Public `AMSL capabilities/capabilities/protocol.mcp-server.json` has `REJECTED / REFERENCE_EXISTING` and points to the official SDK. `identity.mcp-oauth.json` remains CANDIDATE. A successful client or package test is not broad interoperability evidence. T11.14 requires at least two independently implemented clients at frozen versions and separates fixture, actual client and production evidence.

6. **AMSL governance remains independent.** Public `AMSL capabilities/CONTRIBUTING.md` requires human maintainer review for security-sensitive APIs. The public AMSL bootstrap RFC requires guarantees, alternatives, provenance, compatibility and consumer evidence, with maturity promotion separate from a release. T14.10-T14.11 preserve that gate. Owner-enabled AMOS automatic maintenance cannot grant itself AMSL merge/release authority.

The public Go checkout inspected was clean at revision `ae129822f21c51cf77975df804f668dcee6aab9d`. The public capabilities checkout contained uncommitted documentation/catalog work; its working-tree claims must be repinned to reviewed immutable published content at implementation time. No tests in either sibling repository were run or altered. No claim is made about their current production behavior.

## Current standards and tooling

No internet-based claim of current standards or SDK conformance is made by this planning input. Instead, explicit freeze tasks prevent workers from relying on remembered standards:

- T11.1 researches primary official MCP specification and official Go SDK sources at execution time, pins exact revisions/client versions and records supported/excluded authorization features. It must not silently add confidential-client or remote-client-metadata behavior.
- T13.2 checks the selected runtime/model provider's official current credential, usage, cancellation and isolation capabilities before freezing one adapter.
- T15.6 checks official current signing, provenance and CI identity documentation, pins verification tools, and states exact trust roots and rebuild guarantees.

This is intentional protocol revalidation, not a claim that future official sources have already been inspected. Where a task cannot meet its profile with a mature implementation, it stops for a narrow contract amendment instead of inventing a universal protocol.

## Contracts that need deliberate design

**Operation metadata and parity.** OpenAPI shape alone cannot specify authority, workspace selection, entitlement, side effects, retries or proof. T11.2 adds reviewed extension metadata and generation errors for unsupported mappings. Generated hints never become authorization. T11.13 uses the canonical operation inventory to fail on missing applicable MCP access. T12.10 and T12.11 prove an alternate client can use public APIs and proof challenges without private hooks.

**Proof continuation.** T11.12 binds a challenge to principal, workspace, operation digest, required assurance and expiry. Completion is one-time, followed by fresh authorization/entitlement evaluation. The same user cannot prove one transfer and resume a different one. This is application identity proof, not an AMOS customer-agent approval inbox.

**Private business proxy.** T12.4 defines fixed upstream origins, private reachability, authenticated audience/request-bound context, header stripping, canonical path semantics, time/body/response limits and streaming/retry policy. T12.8 separately proves network policy and schedules a real external direct-origin probe through E16. Hiding a URL is not origin isolation.

**Autonomous release state machine.** T13.1 freezes job identities, authority ownership and compare-and-swap transitions. Source, patch, evidence, policy and artifact digests remain immutable. Coding jobs cannot approve themselves, rewrite mandatory checks, modify release identity or obtain production credentials. The independent verifier checks a failing base reproduction and candidate behavior with protected controls. A separate release identity evaluates current owner policy. Unknown outcomes cause reconciliation, not blind retry.

**Diagnostic privacy.** T14.1-T14.3 construct a new typed public payload from allowlisted fields. They do not serialize a raw record and hope redaction is sufficient. Default frames are limited to reviewed public AMOS/AMSL symbols; arbitrary business symbols, source paths, raw messages, addresses, URLs and arguments are excluded. Synthetic secret canaries test encoded and nested channels. Exact preview bytes must match export bytes. Disablement, retention, queue caps and application independence are required.

**Upstream evidence and authority.** Intake is quarantined data, never executable instructions or an implicit job authorization. Public repair requires a clean synthetic reproduction using only public inputs. Suspected security impact and uncertain classification route to a private hold; private business/UI defects stay with the owner. Upstream credentials and policy belong to the upstream owner, not customer installations.

## Concrete adversarial and recovery cases

| Failure or attack | Required task evidence |
| --- | --- |
| Promotion decision replayed for changed source/policy/artifact | T13.1, T13.9, T15.9 |
| Deploy accepted remotely, acknowledgment lost locally | T13.4, T13.10 |
| Canary fails or metrics disappear | T13.11 |
| Database migration makes old binary unsafe | T13.11, T15.13 |
| Restored backup revives revoked credentials | T11.8, T15.13 |
| Malicious log/README/tool output asks for secret/source exfiltration | T13.5, T13.7, T15.11 |
| Diagnostic payload is a decompression bomb or job instruction | T14.4, T14.5, T14.12 |
| Patch pollutes checks indirectly through a generator/build hook | T13.7, T15.7 |
| Candidate deletes tests, skips cases or forges green output | T13.8, T15.8 |
| Third-party PR poisons a cache or supplies a release artifact | T15.10 |
| Valid signature comes from wrong builder/ref or revoked identity | T15.6, T15.9, T15.12 |
| Trusted release omits fresh rebuild evidence | T15.10 |
| Old controller replays resume after owner fleet pause | T13.12, T15.14 |
| Agent, reporter or upstream is unavailable while app serves users | T13.13, T14.3, T14.12 |

## Dispatch and ownership handoff

L14 owns the E11/E12 task paths, L15 the private maintenance paths, and L16 the upstream/security paths. A lane has one active coding task at a time. Multiple tasks listing a package wildcard are sequential within that lane; their ownership is not permission for simultaneous edits. The root coordinator can schedule other lanes independently after frozen contracts. Cross-epic prerequisites are real dependency edges, not an assumption that all sixteen lanes can work simultaneously.

Shared `go.mod`, canonical `api/amos.openapi.yaml`, migration ordering, root entrypoint/router wiring and root workflow files remain with the root integrator. Workers emit fragments in owned paths. Root must resolve `REQUIRES:` dependencies to exact tasks and align proposed API/package names with T1.2 before dispatch. The unresolved descriptions are explicit integration contracts, not optional prerequisites.

Stage entry requires a re-read of the accepted contracts, exact dependency pins, relevant prior-stage evidence and current source state. If a task no longer fits 30-90 minutes after that review, split it while preserving its outcome and negative test. Design tasks need architectural judgment; frozen bounded engineering tasks are intended for lower-cost coding lanes. Estimates exclude cloud wait time, account setup, human review, integration and live qualification.

Verification commands in the JSON are planned commands against files the tasks will create. They have **not** been run. Each Go task lists scoped test, formatting and lint commands; race-sensitive seams add scoped race tests. Browser tasks specify focused Playwright suites. Required database integration tests must fail clearly when their harness is unavailable; skipped tests are not qualification. Heavy multi-package builds follow the shared build-lease rules, with at most the established heavy-build concurrency; this planning task ran none.

E16 owns complete live API/browser/provider/deployment/recovery acceptance. A task's local completion, integration, release and live verification remain distinct states. No automatic deploy, reporting publication, paid model call, upstream PR or source implementation was performed during this planning work.

## Open decisions for the root plan

The JSON includes Q-AM-01-Q-AM-07 with proposed defaults and blocking task IDs: protocol/client profile; issuer storage isolation; private proxy trust; runtime/model/budget profile; automatic change classes; diagnostic retention/private vulnerability handling; build/rollback evidence. These should become root-owned decision/ADR records rather than casual choices made by coding workers. The model cannot grant its own authority, and owner policy cannot override security-sensitive AMSL maintainer review.

## Planning validation and progress

The JSON was parsed successfully and checked for 5 epics, 66 unique task IDs, 40 unique use cases, 30-90 minute estimates, 3-8 instructions per task, valid stages/lanes, full use-case coverage, acyclic internally resolvable dependencies and no internally reversed stage prerequisites. Root/external `REQUIRES:` dependencies remain for root integration. No source behavior or external interoperability has been validated.

Planning progress: E11-E15 input and public-source qualification notes drafted; all implementation, security execution and live evidence remain PLANNED.
