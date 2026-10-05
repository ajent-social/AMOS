# ADR 022: First-class SDLC through complete AWS production acceptance

Date: 2026-10-05. Status: accepted planning direction from the owner; execution and provider qualification remain pending.

## Context

The owner requests the complete SDLC as plan items, maximum parallel GPT-6-Luna agents, and continuation until the entire product is in production at https://amos.sire.run on AWS. Existing product tasks and acceptance records must remain intact; lifecycle prose alone does not provide dispatchable delivery gates. The existing approved scope, use cases and documented privacy/regulatory constraints must govern all dependent work without adding new product scope. Crosscutting design assumptions must be qualified before any source-task preflight, while each subsystem task retains ownership of its specific design.

## Decision

Supplement the canonical product inventory with the [ordinary repository delivery graph](../planning/sdlc-delivery.md). Each remaining versioned deliverable has dependency-linked preflight, implementation, verification, independent exact-head review, GitHub rebase merge and landed verification. Product descendants require verified landing. Previously accepted work retains historical evidence. Add two shared gates: `T-PROD.14` accepts traceability to the existing approved requirements and constraints, and `T-PROD.15` qualifies crosscutting design contracts against that baseline. Every source preflight depends on `T-PROD.15`; the product coverage gate `T-PROD.3` and terminal `T-PROD.13` therefore depend on both gates. These gates do not author replacement subsystem designs or expand scope. First-class environment, release/staging/recovery, production deployment, live verification, observation and final independent acceptance items follow complete product delivery.

One coordinator maximizes eligible GPT-6-Luna workers within actual runtime capacity, ownership and build limits. Execution continues until the final production gate passes or a real prerequisite prevents further ready work; persisted blockers and receipts permit resumption. No new controller or duplicate enrolled PR scheduler is created. Planning adds no deployment receipts or accepted work.

## Consequences

The complete product inventory remains 257 items; the delivery graph has 1,215 nodes: 1,200 lifecycle stages for 200 unaccepted product tasks plus 15 shared/production gates. The original source inventory and acceptance snapshot hashes, all 257 product IDs and the 57 accepted records remain unchanged; `T-PROD.13` remains the sole terminal task. Original execution-state remains the product acceptance authority. New stage receipts need a qualified journal/validator integration before dispatch. One selected AWS profile serves the domain while both profile capabilities retain their qualification requirements. Credentials, installation account/budget/profile, domain control and policy gates are reconciled before affected provider actions. Customer migration or retirement is not inferred from the target URL. Public planning artifacts contain no private resource identifiers or credential values.
