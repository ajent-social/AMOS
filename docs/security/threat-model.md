# AMOS security trust boundaries and threat model

Status: **design only; no runtime control is certified by this model.** The [control matrix](control-matrix.json) is the machine-readable source for stable threat/control IDs, owners, stages, source contracts and future denied-path tests. Every listed control and its planned test remains `NOT_IMPLEMENTED`; the matrix validator checks inventory completeness only. This model follows the frozen [cross-lane contracts](../planning/contracts.md), [RFC 0001](../rfc/rfc-0001.md), and the planned [E15 security qualification](../plans/E15.md).

## Security objective and authority

AMOS is the authorization boundary for its application. A request gains authority only after verified authentication and fresh server-side checks for actor, workspace, membership, grant, entitlement, assurance and resource tenant. Workspaces are independently scoped data/billing compartments, not principals or ambient authority. A browser, API key, OAuth grant, HTTP header, URL, OpenAPI description or MCP argument cannot construct or enlarge a principal. People and machine principals remain distinct. Missing or stale authority information fails closed; responses at sensitive identity/resource boundaries avoid revealing whether another tenant's object exists.

The web UI, REST, generated clients and MCP reach the same domain behavior and policy decision. MCP is a protocol entry point, not an alternate authorization system. An operation that requires human assurance returns a bound, resumable challenge; it does not acquire an agent bypass. AMOS does **not** implement an approval inbox or broad governance policy for a customer's agents. The external agent runtime owns that governance; AMOS still enforces its own application permissions, tenancy, entitlements and proof requirements.

## Principals and trust zones

The boundaries below are distinct even where they share a public domain or deployment. A connection, signature or shared process does not merge authority. The machine-readable principal and zone inventory is in [control-matrix.json](control-matrix.json).

| Zone | What it contains | Boundary rule |
|---|---|---|
| User-controlled browser | Cookies, redirects, local state and client-side rendering | Client-held state is untrusted; CSRF, redirect and tenant selection are checked server-side. |
| Public request edge | Anonymous callers, authenticated people, API-key clients and external MCP clients | Inputs are untrusted until bounded, authenticated and authorized. |
| AMOS application policy | Request normalization, verified principals, current policy decisions and routing | This is the authority decision point; callers cannot inject identity or policy. |
| PostgreSQL data plane and tenant compartments | Identity, grants, migration ledger and independently scoped personal/organization resources and billing | Reads and writes retain tenant/resource checks; transactions and current state govern concurrent changes. |
| External provider callbacks | Identity, payment, email and cloud-provider messages | Validate issuer/signature, audience, state, size and replay constraints before effects. |
| Separate business service | Private upstream origin reached through a narrow proxy | Exact allowlist, authenticated propagated identity and bounded transport; direct public bypass is denied. |
| Owner operations and private maintenance | Owner credentials, logs, policy, pause and recovery controls | Owner app authority stays separate from AMOS/AMSL upstream authority. |
| Coding sandbox | Candidate code, hostile repository text, logs and tool output | No production credentials, protected-check mutation, release authority or unrestricted egress. |
| Independent verifier | Required acceptance checks over exact candidate source | Cannot author the candidate, forge its own evidence or sign/release it. |
| Trusted builder | Qualified build identity and immutable build inputs | Builds only reviewed source with declared toolchain; cannot approve or deploy its artifact. |
| Release authority and owner installation | Current policy, pause generation, target generation and promotion decision | Release identity is separate; installation accepts only matching, current immutable evidence. |
| Owner application repository | Private business code, custom UI, infrastructure and generated ownership baselines | Upgrade tooling detects three-way conflicts and never silently overwrites owner changes. |
| Untrusted input and upstream reporting | External PRs, diagnostics and optional sanitized reporting | Text is data, not instructions; export is typed, allowlisted, bounded and excludes raw/private data. |
| AMSL maintainer and customer-agent runtime | Independent shared-library governance and external customer agent governance | Neither authority is silently delegated to AMOS maintenance or inferred from MCP access. |

## Critical assets

The matrix assigns each asset to its authority zone and maps each crossing to an attacker, decision, owner and negative test plan. The highest-impact assets are credentials and proof challenges; current principal/workspace/grant state; tenant data; subscription, payment and usage state; provider secrets; private source/customizations; diagnostics; immutable source/build/artifact evidence; installed generation and pause state; schema/migration identity; and request/worker capacity.

## Attack inventory and future owner

Each row below has a precise crossing, expected policy decision, linked control and exact planned test path/function in the matrix. Task/test assignments are future work, not evidence that the tests or controls already exist.

| Threat | Material attack | Planned control owner |
|---|---|---|
| `THR-TENANT-CROSSOVER` | Valid identity selects another person's workspace, stale membership/grant or another tenant's resource. | [T15.2](../tasks/T15.2.md): current membership, entitlement and resource-tenant denial. |
| `THR-SESSION-REVOCATION` | Stolen, expired or revoked session is used after account/credential lifecycle change. | [T3.3](../tasks/T3.3.md): current opaque-session state before protected access. |
| `THR-PROOF-CHALLENGE-REPLAY` | A challenge is replayed or resumed under another actor, tenant or operation. | [T3.17](../tasks/T3.17.md): exact-bound resumable proof and fresh post-proof policy. |
| `THR-KEY-MCP-TENANT-ESCALATION` | API key, OAuth grant, client argument or private MCP discovery widens machine scope or crosses tenant. | [T15.2](../tasks/T15.2.md): same current application authority for machine and MCP entry points. |
| `THR-WEBHOOK-REPLAY` | Forged, duplicate or conflicting provider event causes an unverified or repeated durable effect. | [T5.7](../tasks/T5.7.md): signature, durable receipt and replay/digest checks. |
| `THR-OUT-OF-ORDER-BILLING` | Delayed event or stale worker restores old paid access or repeats an uncertain money effect. | [T5.8](../tasks/T5.8.md): authoritative reconciliation and stale-result denial. |
| `THR-CALLBACK-STATE-REPLAY` | Cross-site, replayed or issuer-mismatched callback creates a session or links an identity. | [T3.10](../tasks/T3.10.md): browser/provider-bound proof and identity-linking checks. |
| `THR-PUBLIC-REQUEST-ABUSE` | Host spoofing, CSRF, oversized input, ambiguous encoding or unbounded anonymous requests cause mutation or exhaustion. | [T15.3](../tasks/T15.3.md) and [T15.5](../tasks/T15.5.md): bounded rejection before side effects. |
| `THR-BROWSER-CSRF` | Hostile site/browser context submits a cookie-authenticated mutation or supplies tenant/redirect state. | [T15.3](../tasks/T15.3.md): CSRF and redirect rejection before side effects. |
| `THR-PROXY-SSRF-IDENTITY-SPOOF` | Proxy destination/redirect or forged identity/forwarded-host headers bypass the app boundary. | [T15.3](../tasks/T15.3.md): private origin, allowlist, credential stripping and authenticated identity propagation. |
| `THR-PARSER-GENERATOR-DIVERGENCE` | URL normalization or generator/schema drift drops reserved-route or operation policy checks. | [T15.5](../tasks/T15.5.md): deterministic alternate-encoding and fuzz denial cases. |
| `THR-SECRET-DIAGNOSTIC-LEAK` | Attacker-controlled logs or crash text carries secrets/business data into reporting or browser artifacts. | [T15.4](../tasks/T15.4.md): typed diagnostic allowlist and synthetic canaries. |
| `THR-HOSTILE-MAINTENANCE-EGRESS` | Malicious log, README, issue, commit or tool output asks a coding agent to exfiltrate secrets/source or change job scope. | [T15.11](../tasks/T15.11.md): adversarial egress checks in the actual sandbox. |
| `THR-CODER-SELF-VERIFICATION` | Candidate deletes, forges or rewrites its own acceptance evidence to obtain release authority. | [T15.8](../tasks/T15.8.md): protected checks with independent verification. |
| `THR-UNTRUSTED-PR-CACHE-POISON` | External PR receives privileged secrets, poisons trusted cache, or supplies a release artifact. | [T15.10](../tasks/T15.10.md): isolated validation and trusted post-review rebuild. |
| `THR-RELEASE-IDENTITY-COMPROMISE` | Wrong/revoked identity or valid signature alone is mistaken for approval of an artifact. | [T15.9](../tasks/T15.9.md) and [T15.12](../tasks/T15.12.md): immutable source/artifact binding, revocation and incident hold. |
| `THR-STALE-RELEASE-REPLAY` | Release A's approval is replayed over newer release B or changed current policy. | [T13.10](../tasks/T13.10.md): compare-and-swap on expected installed generation plus current policy. |
| `THR-PAUSE-PROMOTION-RACE` | Eligibility or promotion races with owner pause; an old controller resumes or replays work. | [T13.12](../tasks/T13.12.md): persisted pause generation checked at launch and promotion. |
| `THR-RESTORE-REVOKED-AUTHORITY` | An old backup restores revoked credentials, grants or maintenance authority. | [T15.13](../tasks/T15.13.md): independent authority epoch/freshness barrier before activation. |
| `THR-CUSTOM-UPGRADE-OVERWRITE` | Upgrade treats customized business/UI/infrastructure files as generated and overwrites them. | [T9.8](../tasks/T9.8.md): ownership baseline and three-way conflict stop. |
| `THR-MIGRATION-SOURCE-SUBSTITUTION` | Applied migration bytes change under an existing ID or concurrent migration applies ambiguous schema state. | [T1.3](../tasks/T1.3.md): transactional checksum ledger and changed-source denial. |

## Maintenance authority and privacy boundaries

Owner-account maintenance and AMOS upstream maintenance are separate loops. Private app code/custom UI/infrastructure stays within owner-controlled execution. Upstream reporting receives only a new typed payload built from an explicit sanitized allowlist; redaction alone is not a guarantee. Raw logs, business data, secrets, arbitrary source paths, provider messages and identifying infrastructure details are excluded. Reporting, upgrade automation and maintenance-agent availability must not determine whether the owner application serves requests.

Coding, verification, build and release are separate principals with separate scoped credentials. The coder has no production secret or release credential and cannot edit policy, protected tests or required-check authority. The verifier checks exact candidate source. The builder binds exact reviewed source and declared toolchain to immutable artifact digest. The releaser rechecks current policy, revocation, pause and expected installation generation at the promotion compare-and-swap. Owner pause/revocation works independently of the model or coding agent. A pause race denies subsequent promotions; unreachable installations are reported as unknown rather than presumed paused. Resume requires fresh reconciliation and policy/evidence, not a blind retry.

Database recovery and application rollback do not roll authority backward. Revocation/authority epochs need a source outside the restored snapshot or a freshness barrier before enabling traffic. Unsupported schema/release compatibility or uncertain restore remains held. Upgrades use a versioned ownership manifest and three-way comparison; changed owner files remain intact until an explicit owner resolution.

## Qualification state and residual risk

The matrix's sole implementation state is `NOT_IMPLEMENTED`. Its exact denied-path functions are **planned test names**, not existing tests. The `internal/securitycontract` package validates the inventory schema, owners, links and required threat coverage; it does not exercise authentication, tenant isolation, proxying, payment providers, sandbox egress, artifact provenance, recovery or promotion. No provider, browser, cloud, release, restore or independent security-review evidence is claimed here. E15 and E16 keep those implementation and live-qualification gates separate.

The customer agent-governance policy, autonomous AMSL merge authority, and automatic security-sensitive policy changes remain outside AMOS. External provider assurance, live topology isolation, credential storage, runtime sandboxing, durable pause, release signing, and restore freshness remain unqualified until their mapped tasks produce reproducible positive and denied-path evidence.

## Open decisions and proposed ADR fragments

These choices remain explicit implementation gates from the RFC and planning record; this threat model does not decide them:

- **Authentication and recovery authority:** exact storage/session adapter, IdP assurance mapping, organization-enforced MFA/recovery semantics, and the authoritative revocation epoch/freshness source across restore. Identity tasks T3.1, T3.17, T3.23 and recovery task T15.13 must preserve fail-closed behavior until the owner-approved choices are qualified.
- **Ingress and service identity:** exact forwarded-host/proxy trust configuration and authenticated identity propagation for a separate business service; Cloudflare DNS-only versus reverse-proxy behavior remains a separate topology contract. T15.3 and the deployment tasks must verify the selected live profile.
- **Maintenance isolation and release roots:** actual coding sandbox/runtime, permitted network egress, verifier identity, builder/toolchain provenance, releaser credential and target-generation CAS adapter. T15.6, T15.8–T15.11 and T13.10 must name real trust roots and exercise the chosen runtime rather than fixtures alone.
- **Diagnostic privacy policy:** exact typed export allowlist, user disablement, retention and volume bounds. No raw-log export is a default; T14 and T15.4 must supply canary-backed evidence before enabling reporting.

Proposed root-owned ADR fragments:

1. **Separate maintenance identities and promotion authority.** Record the coder/verifier/builder/releaser capability split, credential scopes, required immutable source-to-artifact binding, current-policy check, target-generation compare-and-swap, independent owner pause and recovery behavior. Define which decisions require explicit human review and which unknown states stop promotion.
2. **Keep authority outside rollback-prone state.** Record where revocation/pause generations live, how restored application data re-establishes current authority, and the activation hold used when freshness cannot be proved.
3. **Make upstream diagnostics opt-in and structured.** Record the allowlist schema, excluded raw/business/secret fields, size/retention bounds, disablement, and the exact canary channels that must remain empty.

Root owns ADR creation and plan progress. These are proposals for that review, not accepted amendments to the frozen contract.
