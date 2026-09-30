# Identity, tenancy, billing and shared UI planning input

This is a public planning artifact, not an implementation or qualification report. The companion [task input](../inputs/identity-billing.json) inventories E3-E6, 75 tasks and UC-020-UC-049. Every task is planned for 30-90 minutes, carries explicit owned paths, dependencies, concrete acceptance, a scoped verification command and a meaningful negative case. Proposed package names, routes and test names become executable contracts only after the foundation freeze. No implementation, build, test, provider call or deployment was performed for this research.

The input follows [VISION](../../VISION.md) and [RFC 0001](../../rfc/rfc-0001.md). Full later-stage inventory is intentional; later contracts must be revalidated against completed prerequisite decisions before dispatch. The plan and Go skills informed task structure and library boundaries. Their generic requirement to include production verification is fulfilled by the separate E16 release gate, not by granting coding workers production authority.

## Scope and sequence

E3 owns people, credential proofs, browser sessions, email verification, password/password recovery, magic links, Google, GitHub, Apple, passkeys, MFA/recovery, proof challenges, safe linking, profile/session management and account lifecycle. E4 owns personal and organization workspaces, current membership and resource authorization, invitations, roles, ownership, organization OIDC, MFA enforcement and organization lifecycle. E5 owns independent workspace billing, flat monthly/yearly plans, seats, immutable usage, Stripe integration, verified receipt, reconciliation and financial lifecycle. E6 owns default lifecycle pages and public contracts for themes, individual overrides and complete UI replacement.

S1 is a local first paid-application slice: password and email proof, revocable sessions, one personal workspace, approved flat recurring plan, test-provider checkout, verified payment projection and a protected business action with usable default pages. This is a subset of the complete contract. S2 qualifies actual test-mode provider callbacks in an authorized disposable deployment. S3 completes selected identity, organization, seat/usage and replacement UI capabilities. S4 verifies upgrade/restore compatibility. S5 maintenance should consume these versioned policy, proof and compatibility contracts; it must not reimplement authentication or billing or give a coding worker authority to weaken checks.

Independent lanes after contract freeze are L03 identity primitives/lifecycle, L04 provider/enterprise/MFA, L05 tenancy, L06 billing, L07 seats/metering and L08 shared UI. Each lane owns domain fragments; foundation integrator owns module dependencies, OpenAPI aggregation, migration ordering, application wiring and root workflows. A task consuming another domain uses its public interface and dependency, not a sibling's internal package. Test fixtures remain outside production paths.

## Public source inspection

Public AMSL source was inspected at local `ajent-social/go` revision `ae129822f21c51cf77975df804f668dcee6aab9d`. Its worktree was clean at inspection. Public catalog records and source docs label the relevant implementations **CANDIDATE**. Catalog references may point to an older immutable revision than current local source; qualification must pin the reviewed revision and avoid treating a newer checkout as previously reviewed evidence.

| Capability | Public evidence inspected | Conclusion for AMOS |
| --- | --- | --- |
| Person records | `ajent-social/go:accounts/accounts.go`, `docs/accounts.md`, `accounts/accounts_test.go` | Thin subject registry; not organization or lifecycle composition. Qualify its transaction fit rather than assuming it supplies atomic signup plus workspace bootstrap. |
| Password/session | `ajent-social/capabilities:capabilities/identity.password-hashing.json`, `identity.sessions.json` | Hashing is REJECTED/REFERENCE_EXISTING; sessions are DISCOVERED/REFERENCE_EXISTING. Evaluate maintained crypto/session libraries, not a new AMSL framework. Password reset is not password authentication. |
| Magic link | `ajent-social/go:magiclink/magiclink.go`, `magiclink_test.go`, `docs/magic-link.md` | Hashed one-use email challenge and mailer seam; caller owns delivery reliability, session, CSRF, abuse controls and assurance. |
| Social identity | `ajent-social/go:oidc/oidc.go`, `oidc/github/github.go`, `oidc/apple/apple.go`, `docs/oidc-social.md` | Core OIDC and separate GitHub OAuth adapter exist; caller owns browser binding, linking proof, current account status and session minting. Apple helper alone is not complete Apple web sign-in. |
| Passkeys | `ajent-social/go:passkey/passkey.go`, `passkey_test.go`, `docs/passkey.md` | Configured RP/origins and consume-before-verify ceremony exist. Complete positive browser ceremonies, account lifecycle and method assurance still require composed evidence. |
| Checkout | `ajent-social/go:billing/checkout/checkout.go`, `checkout_test.go`, `docs/billing-checkout.md` | Durable attempts, conflicts and unknown-response recovery are useful candidates. Map caller account identity explicitly to workspace billing owner. Redirect is not payment proof. |
| Subscription | `ajent-social/go:billing/subscription/subscription.go`, `subscription_test.go`, `docs/billing-subscription.md` | Durable inbox/CAS projection is useful but does not establish authoritative current price/items, freshness or replacement-subscription semantics. AMOS needs provider snapshot reconciliation and application policy. |
| Entitlement | `ajent-social/go:billing/entitlement/entitlement.go`, `docs/billing-entitlement.md` | Status allowlist is not complete product plan/tenant/resource/freshness policy. Do not equate provider availability failure with known unpaid denial. |
| Usage | `ajent-social/go:usage/usage.go`, `docs/usage-meter.md` | Per-owner bounded CAS counter is not immutable financial event ledger, meter delivery, corrections or invoice reconciliation. |

The inspected OIDC `Pending` struct and `Start`/`Accept` methods contain state/nonce handling but no PKCE verifier. T3.22 proposes a narrow S256 authorization-code binding improvement using existing OAuth helpers, backed by independent synthetic tests, a separate upstream worktree, maintainer security review and immutable release. Browser flow binding, organization registry, proof policy and UI remain AMOS. If an upstream release is unavailable, evaluate qualified local composition with maintained libraries rather than weakening the flow or copying restricted code.

Historical public tests inspected include account concurrent creation, magic-link consume-once, passkey consume-before-verify, checkout unknown-response recovery, subscription duplicate/conflict/out-of-order cases and SQL store tests. These were **read, not run**. Some SQL tests skip when PostgreSQL is unavailable. A future qualification gate must provision the actual selected database, reject skips or zero matching tests and record actual cases executed. Presence of tests or catalog PASS labels does not qualify AMOS composed behavior.

No restricted implementation or identifying restricted evidence is necessary to execute this plan. Public upstream improvements and regression fixtures must stand on public contracts and independently authored synthetic behavior.

## Boundary decisions

One person has one personal workspace and may join multiple organizations. Each workspace is a separate billing owner. Stable IDs, current membership and resource ownership control authority; email equality, email domain, UI controls and a paid subscription cannot establish tenant permission. Session workspace is only a selection hint. Browser, API and MCP must re-evaluate current account/workspace/membership state with the accepted consistency bound.

A sensitive operation can require fresh primary proof, MFA or organization IdP proof. API/MCP must return a typed resumable challenge bound to actor, tenant, operation, parameters and grant; subsequent completion rechecks current policy. It cannot turn an agent token into permission to bypass a human proof. Per-organization SSO has explicit issuer/client/configuration binding and a reviewed membership provisioning rule. Default proposal is preprovisioned membership; domain verification does not confer membership.

Billing provider transport and durable coordination are separate from product entitlement policy. Webhook signature verification precedes durable receipt and acknowledgement. Event arrival is a reconcile trigger; provider current snapshot plus approved price/period/freshness governs access. Replacement subscriptions, unmatched events, retries, missed events and mode/account boundaries require synthetic failures that exercise real request and database boundaries. Seat desired state, confirmed provider state and pending reservations remain distinct. Usage is immutable and deduplicated before aggregation or provider dispatch.

The default UI calls shared domain behavior through public contracts. A renderer override must not become an alternate auth engine. Complete replacement remains supported, including required CSRF, proof challenges and error states. Upgrade planning checks declared UI/security contract compatibility and authored-file preimages; missing required proof cannot be called a successful compatible upgrade.

## External and cross-epic integration requirements

Root must resolve these `REQUIRES:` edges to concrete task IDs before dispatch:

- Qualified email adapter, transactional outbox, durable jobs and object/export storage contracts.
- Shared operation policy dispatcher plus API/MCP principal, grant and generated contract semantics. The minimal paid-action dispatcher must exist in S1; complete protocol exposure can follow later without duplicating domain logic.
- Security audit event sink, approved retention and visibility rules.
- Authorized disposable preview, actual email delivery and provider callback qualification.
- Backup/restore activation barriers, upgrade compatibility and managed-file three-way ownership harnesses.

Provider fixture success, actual sandbox qualification, deployment and production release are separate evidence states. T3.21 covers individual identity provider setup and genuine callback/device behavior. T5.10 covers actual Stripe test-mode flat checkout; T5.20 covers actual seat/meter/invoice behavior. A missing Apple registration, approved sender, IdP or Stripe meter must remain an explicit blocker. No production deployment or real charge is authorized by any engineering task. E16 owns approved production release and live verification.

## Review concerns to resolve before implementation

The highest-impact open decisions are password/session parameters and revocation bounds; MFA/recovery and enterprise assurance; initial email/storage adapters; installation administrator authority; exact plan/price/tax/grace/refund/dispute policy; seat counting and reservation semantics; usage units/corrections/cutoffs; and accepted accessibility/UI compatibility targets. Defaults in the input are recommendations, not founder-selected facts. Record accepted decisions through the root ADR process before durable contracts or provider behavior depend on them.

Before dispatch, the integrator should check dependency stages and task estimates against the actual frozen APIs, ensure every newly exposed operation has real HTTP assertions, and split any task that remains over 90 minutes after fixture/harness setup. Scoped planned commands require named tests that actually execute; successful exit with zero tests or dependency skip is not acceptance. Formatting and scoped lint/vet are required for changed packages under the root validation workflow. Shared build-load/lease policy applies to any multi-package verification; this planning work did not acquire a lease or run builds.
