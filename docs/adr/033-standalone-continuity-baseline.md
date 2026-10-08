# ADR 033: Independent continuity reference application

Status: proposed; independent exact-head review and adoption required.

## Decision proposed

Add a bounded, provider-independent continuity reference journey without removing
any existing product or production task. Keep application-owned business data,
rules, definitions, migrations and procedures separate from generic foundation
mechanisms. Reuse the qualified local host at its existing evaluation boundary.
No production constructor, new principal authority or shared operation executor
is inferred from that reuse.

The acceptance mapping is [replacement baseline](../planning/replacement-baseline.md).
The first source contract is [continuity domain](../contracts/continuity-domain.md).
This is an additive application contract; it does not allocate a shared schema
migration or change the frozen identity/operation wire contract. Exact host
composition and authority admission require a separate adopted contract before
HTTP or persistence source is dispatched.

## Consequences

Pure domain rules can be built and reviewed independently while current-session
and host seams are qualified. They grant no authority and cannot perform effects.
The local baseline does not require live payment or external agent access. Full
billing, organization, API/MCP, hosting and operational scope remains preserved.
Generic feature/view/operation tooling and evolution require their own contracts;
this reference app cannot silently freeze the broader draft RFC.

Replacement is only asserted after the complete mapped isolated journey passes.
Deployment and consumer cutover remain separately authorized and evidenced.
