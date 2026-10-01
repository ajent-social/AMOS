# AMOS next phase: agent-built business features

Draft supporting discussion — 2026-10-01. [RFC 0002](../rfc/rfc-0002.md) governs the current proposal and lifecycle requirements; historical design notes below are not acceptance gates. No implementation or deployment approval is implied.

## Agreed direction

Business owners describe requirements to agents. Agents author versioned declarative definitions in the application's Git repository. Definitions generate ordinary application code delivered through the normal AMOS process. NocoBase is conceptual inspiration; this proposal does not embed its runtime or reuse its components.

The reference application is provisionally called Operations Desk. It first serves the owner's cloud accounts, then ships as a separate customer-owned AMOS installation. Initial providers are AWS and Cloudflare. AWS visibility covers the main payer and authorized linked accounts, across regions. Product-development visibility follows cloud visibility.

Use interchangeable provider adapters and preserve reasonable hosting/resource portability. Portability means supported alternatives, capability discovery, exportable configuration and data, and explicit migration plans. It does not promise that every managed resource or operational semantic transfers unchanged.

The selected visual direction is the dark Operations Desk mock. Integrate a floating glass chat connected to a dedicated organization chief in an external agent runtime. The external runtime owns chat execution, agent delegation, governance, and approvals. AMOS owns application authentication, permissions, contracts, presentation, and lifecycle. Customer installations never share a chief or conversation implicitly.

## Architectural boundaries

| Owner | Responsibility |
|---|---|
| AMOS core | Application foundation, identity/workspaces, policy enforcement, contract/code generation, delivery and maintenance |
| Feature-generation tooling | Validate definitions and compose generated business code against public AMOS contracts |
| Operations Desk modules | Cloud inventory, spend, utilization evidence, recommendations, and later development projections |
| Provider adapters | Discovery, observation, cost ingestion, supported mutations, and provider-specific details |
| External agent runtime | Conversations, chief/specialist routing, tasks, governance, approval decisions and execution policy |

Cloud business concepts do not become mandatory AMOS-core concepts. Feature definitions compose tested implementations; they do not make arbitrary generated cloud mutations trustworthy. Business web/API/MCP interfaces use the same application behavior and authorization. Presentation cannot grant authority.

## Declarative feature model

Begin with a small vocabulary driven by this application rather than a universal no-code platform:

- Data: entity schemas, relationships, validation, workspace scope and provenance.
- Operations: typed inputs/outputs, OpenAPI identifiers, permissions, entitlements and API/MCP exposure.
- Views: routes, approved component references, data bindings, formatting and explicit loading/empty/error/denied states.
- Jobs: supported ingestion tasks, schedules, checkpoints, freshness and partial-failure reporting.
- Design: theme tokens, component versions and reviewed compositions.

Retain OpenAPI as AMOS's operation contract. Define where additional feature metadata is authoritative and derive outputs from it; do not maintain conflicting permission or operation definitions. Generated files carry ownership markers. Authored business implementations and component overrides remain protected from regeneration.

Produce ordinary Go business modules, templates, HTMX and CSS through public AMOS seams. The existing independent-service option remains available for business implementations that require it. The declarative format and generation tooling are proposed additions, not existing qualified capabilities.

## Cloud model and provider portability

Normalize provider/account/location identity, resources, relationships, observations, cost records and recommendations. Retain provider-native identifiers and bounded details alongside normalized fields. Record collection time, observation period, source, currency, coverage and missing permissions.

Separate resource inventory from cost attribution. Account/service costs may exist without a reliable per-resource allocation. Show unattributed spend rather than assigning invented precision. Distinguish actual, estimated and forecast amounts. Do not equate low CPU with safe resizing, or unknown metrics with zero utilization.

Adapters advertise supported discovery, metrics, pricing and mutation capabilities. An unsupported capability is explicit. Comparisons use compatible units and observation windows; avoid a universal utilization percentage across unrelated resource types.

Resource-to-product grouping uses explicit mappings, tags, deployment metadata or reviewed suggestions. Unassigned resources stay visible. Every inventory page includes coverage and freshness so a partial scan cannot imply that everything has been discovered.

Hosting portability and managed-resource migration are separate concerns. Maintain provider-specific deployment implementations behind AMOS deployment profiles. Introduce cross-provider resource migration only for named, tested pairs, with compatibility, data transfer, downtime, cost, verification and rollback documented.

## Live web chat with an external runtime

Bind each installation/workspace to a verified external runtime installation, organization, designated chief and authorized conversation. The browser must not receive privileged controller credentials. Resolve individual user identity and authorized conversation access; an AMOS login is not automatically an external runtime grant.

Use the qualified controller adapter's conversation operations for admitted messages and durable history. Use its defined reply-preview stream where supported. Previews are transient; durable history and committed task/review state remain authoritative. Browser reconnect must not submit a message twice or render preview text as approval/execution evidence.

The chief is the default conversational counterpart. Proposed cloud and development specialists receive scoped delegation inside the external runtime. Specialist identities remain visible where useful, but the user need not manually choose a worker for every question.

Offer explicit context chips for the selected product/resource. Send stable references and bounded authorized context, not unrestricted screen contents or secrets. Preserve the difference between a suggestion, an approval request, an approved action, execution, and verified completion.

A cloud mutation must satisfy both external runtime governance and the application's current permission/provider constraints. A free-text reply, clicked decorative button or stored UI flag is never approval proof. Validate the exact supported integration contract before implementing the approval handoff; do not invent a second AMOS approval inbox.

## Design and review workflow

1. Review visual direction and representative screens.
2. Review a clickable prototype and component gallery.
3. Review complete feature flows, data semantics and failure states.
4. Explicitly approve production implementation and Git commits.

Version approved design tokens and components. Definitions reference these components and their bindings; new components and significant visual changes return for review. Agree separately on how much reuse can proceed automatically after the initial approval.

The glass-chat source uses HTML/CSS/HTMX, translucent bubbles, backdrop blur, a composer, a history control and accessibility markup. Adopt floating bubbles and a lower-right composer that can expand to readable history without permanently covering the resource inspector. Provide keyboard navigation, readable contrast, reduced motion, and a narrow-screen mode. The reference's sample replies are a demo, not a live external runtime integration.

Exact visual adaptation is pending a screenshot of the reference. Browser policy blocked opening its local file URL; source inspection does not establish visual fidelity. The selected generated mock also contains illustrative metrics that must be corrected before becoming an implementation specification.

## Suggested delivery slices

1. Design prototype: selected shell, resource inspector, glass chat and component states using synthetic data; review before production implementation.
2. AWS inventory: connect the payer and authorized linked accounts, list supported resource types across regions, show explicit scan coverage, unknowns and unassigned resources.
3. Spend: account/service totals, trends and defensible attribution with separate billing freshness.
4. Utilization: resource-specific metrics, observation windows and evidence-based recommendations.
5. Live chat: bind the dedicated chief, authorized history, durable message admission, streaming previews and explicit resource context.
6. Approved changes: one bounded, reversible cloud action through qualified external runtime governance and application authorization, with verification of outcome.
7. Cloudflare: inventory/spend/metrics appropriate to supported services behind the same capability contracts.
8. Development: source-backed plans and task-status views; map projects to products and resources without making AMOS a second task controller.
9. Customer installation and portability proof: independent installation, configuration export/recovery, a second provider adapter or deployment target demonstrating actual interchangeability.

Cloudflare and chat can move earlier if the owner's priorities justify it. Keep integrations staged against the existing AMOS foundation rather than claiming the new application bypasses its remaining delivery and release work.

## Remaining decisions

- Source of truth for development status: repository plans, GitHub issues/projects/PRs, external controller tasks, or defined combinations with explicit precedence.
- Exact required AWS resource coverage, acceptable inventory/metrics/cost freshness and first approved mutation.
- Reference-chat screenshot and detailed component approval/reuse boundaries.
- Exact supported AMOS/controller identity, hosting, authentication and approval integration; verify end-to-end rather than infer from contracts.

## Evidence inspected

- AMOS: docs/VISION.md, docs/runtime.md, docs/codegen.md, docs/contracts/ui.md and docs/plans/E6.md.
- NocoBase: v2 BlockModel/PageModel and FlowEngine composition, database Collection, data-source-main and workflow plugin structure.
- External controller behavior is a proposed integration requirement, to be qualified through supported public contracts and real admission/history/stream/approval checks; private source and registries are not public AMOS evidence.
- Glass chat: dist/chat.html, dist/chat.css and dist/chat.js. Actual browser rendering was blocked.

These records establish architectural intent, not a qualified external-controller seam, release qualification or a working integration. No live cloud credentials, customer data or external changes were used for this proposal.
