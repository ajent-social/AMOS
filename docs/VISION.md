# AMOS vision

Recorded: 2026 09 30 UTC. Product direction and proposed implementation boundaries. This document and RFC 0001 supersede conflicting early brainstorming proposals. Current implementation status is in the execution registry and roadmap.

## Purpose

AMOS is an open-source, agent-maintained application foundation. A developer creates a repository, runs `amos init`, reviews the generated configuration, then runs `amos deploy` to bring a complete SaaS foundation online at their own domain. They concentrate on business features instead of rebuilding accounts, subscriptions, operations and agent access.

AMOS is intended for independent developers and learners building their own business or client applications. The first product outcome is a new paid SaaS application. Existing-app adoption remains valuable later. Teaching should help participants understand and own the software; an automatic deployment is not evidence of that understanding.

## The experience

The initializer asks about the application, AWS, Cloudflare, DNS and relevant provider configuration. It prepares frontend and backend code, GitHub Actions workflows and infrastructure as code. The complete target includes sign-up/sign-in, profiles, personal and organization workspaces, invitations, roles, account administration and subscriptions. Provider registration and account approval remain visible prerequisites rather than silently fabricated success.

AMOS owns the public request boundary. At one public domain, shared routes such as `/signin` belong to AMOS while the landing page and business routes belong to the business app. The business implementation may be integrated into the Go app or run as a separate service in another stack. Internal routing does not expose an alternate business-service hostname or path to users.

AMOS owns identity, workspaces, policy, audit, billing and API/MCP application capabilities, and remains useful during external workflow outages. A code-change lifecycle service owns PR safety and first-class executable author/review/fix/re-review/landing tasks. A product workflow controller owns product delivery and qualification. AMOS retains owner-local diagnosis, proposal and upgrade generation, and bounded domain jobs without implementing another task/PR orchestrator. Release and upgrade profiles are immutable and versioned. Restricted implementation source is not transplanted into this public repository.

The default stack is Go, server-rendered HTML, HTMX and plain JavaScript/CSS. Business behavior is defined contract-first in OpenAPI. AMOS generates Go API scaffolding and MCP tools, with explicit permissions, entitlement requirements and exposure metadata. The web interface and generated interfaces reach the same application behavior. Later clients can use the APIs without redefining access rules.

## Confirmed scope

- Each attendee or client owns and runs their AMOS installation.
- AWS first; Cloudflare required. Support both managed containers with managed PostgreSQL and a smaller VM hosting the app and PostgreSQL, selected per installation. Each profile states its cost, availability, maintenance and recovery tradeoffs. Cloudflare proxy mode remains an implementation decision.
- Both an integrated Go business app and an independently deployed service are supported.
- Complete default web application plus APIs and MCP first; end-user mobile, CLI and SMS clients later. The developer `amos` CLI is required now.
- Personal accounts can create or join organizations. Personal workspaces and organizations have separate subscriptions.
- Flat monthly/yearly, per-seat and usage-based billing; Stripe first through an adapter permitting future providers.
- Email/password, email magic link, Google, GitHub, Apple, passkeys and per-organization OIDC enterprise SSO. SAML is not selected for this scope.
- MFA, including organization-enforced MFA.
- Customer API keys and MCP OAuth; agents can access all applicable application functions subject to ordinary permissions, grants and subscription policy.
- Independent agents, including agents governed by external runtimes, connect through standard MCP. AMOS does not host external agent runtimes or require a particular one. Future agent APIs remain an extension direction, not a speculative protocol to implement now.
- AMOS enforces application security. External agent runtimes own broad agent governance. Do not add an AMOS approval inbox for customer agent actions merely because the old RFC or early discussion proposed one.
- Shared pages are fully replaceable. An upgradable core, optional default UI, developer-owned code and managed deployment files form a hybrid distribution.
- Automated upgrade pull requests belong in the first release scope.
- Operational tooling includes deployment, monitoring, backups and recovery.
- Self-improvement observes metrics, logs and crashes; owner-local agents may investigate, propose bounded domain jobs and generate upgrades. Code-changing PRs use the external lifecycle service with independent exact-head review and verified landing; product delivery and qualification remain separately controlled.
- Shared AMOS code/infrastructure fixes go upstream. Business-code and custom-UI fixes stay in the application's private repository.
- Application maintenance executes in owner-controlled infrastructure with owner model credentials and spending limits. AMOS upstream has its own maintenance system. Managed application maintenance is deferred.
- Default upstream reporting contains sanitized diagnostics only: versions, bounded metrics, error signatures and redacted stack information; no raw logs, business data or secrets.
- Plan the entire architecture and deliver it in stages. Preserve this record so a new session need not reconstruct the conversation.

## The AMSL relationship

Recommendation under active architecture planning: build AMOS on qualified AMSL Go, Pulumi and delivery capabilities. AMOS supplies real integration evidence and narrowly scoped improvements upstream. AMSL owns reusable mechanisms; AMOS owns the application composition, user experience, policy and lifecycle. Do not expand AMSL into a SaaS framework or wait for every possible library improvement before shipping a bounded AMOS slice.

The catalog, tests and consumer evidence determine what can be reused. Candidate status does not imply production qualification. Standard-library and mature external implementations remain valid choices when they already solve the problem. A request to consider reuse is not approval to copy restricted code or bypass upstream maintainers.

## What quality means

An installation must demonstrate allowed and denied actions, tenant isolation, revocation, honest dependency failure, verified payment state, repeatable deployment, recovery, ownership of costs and credentials, and evidence for each advertised interface. Every capability carries its explanation, operating limits and meaningful checks. No blanket compliance certification or universal enterprise-readiness claim follows from including SSO.

Autonomy is bounded by policy, credentials and independent verification. A coding agent cannot grant itself permissions, rewrite its own mandatory checks or directly obtain production authority. Unsupported or uncertain changes stop or escalate. Application runtime continues to work when telemetry submission or maintenance agents are unavailable.

## First reference milestone

Prioritize a genuinely working, rehearsed application slice. A narrowed demonstration is not permission to drop confirmed platform requirements or label fixtures as live providers. Preserve a useful local fallback and report which features are implemented, provider-qualified, deployed and rehearsed separately. Individual training schedules and attendee information do not belong in this public repository.

## Still to decide or qualify

License for original AMOS work and reuse provenance; first reference business feature; exact deployment topology and cost envelope; Cloudflare proxy behavior; email provider; database and session implementation; plan/price/grace/tax semantics; MFA recovery/enterprise assurance rules; maintenance policy defaults and diagnostic retention. Planning may recommend defaults but must label them and gate sensitive execution on resolution.
