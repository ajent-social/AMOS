# Replaceable web presentation contract v1

The default UI is Go server-rendered HTML with HTMX and plain JavaScript/CSS. These contracts describe required behavior, not completed pages. Business landing and business-feature routes remain application-owned. Replacement themes, individual pages and the entire frontend use the same public operation/API contracts; presentation never becomes an authorization boundary.

## Routes and states

AMOS reserves authentication, callback, account, workspace, billing, API, MCP, readiness and health namespaces according to the runtime registry. Business registration colliding with those roots fails initialization. A business route may not shadow sign-in or an encoded equivalent. The root landing page belongs to the business application.

Shared pages cover sign-up, sign-in, signout confirmation, contact verification confirmation, password reset initiation/confirmation, method callbacks, passkey enrollment/sign-in, MFA/recovery challenges, personal profile/account administration, workspace chooser, organization membership/invitations/authentication settings and billing catalog/subscription/payment-action states. Unqualified methods/providers are visibly unavailable; a rendered button never pretends its adapter is active.

Every page explicitly distinguishes anonymous, authenticated, forbidden, missing/nonenumerating, invalid-input, dependency-unavailable, operation-pending, proof-required and success states. Organization selectors require current server-side membership/grant resolution. Billing/account/recovery routes remain usable without paid-feature access where their own policy permits them. Organization policy and current assurance are enforced by the service runner, not hidden-menu state.

## Required page inventory and states

Page IDs below have major version 1. Every row must render a pending/loading state during a declared async operation, an explicit unavailable state on dependency failure, a nonenumerating denied state, and the listed initial/empty and success outcomes. Loading never disables recovery navigation or implies success; full-page forms remain usable without JavaScript. HTTP/API policy supplies authoritative status. Authentication pages use generic responses where contact/account enumeration would otherwise occur.

| Page ID | Reserved route | Initial/empty state | Success state | Mutation and security requirements |
|---|---|---|---|---|
| signup | `/signup` | Blank enrollment form | Verification pending | POST; origin/CSRF enrollment transaction; no session before verification |
| signin | `/signin` | Methods supported by current installation | Authenticated return | POST; browser-bound login CSRF; generic failure; qualified methods only |
| signout | `/signout` | Confirmation | Revoked session, signed-out landing | POST only; current cookie origin+CSRF |
| verify-email | `/verify-email` | Link confirmation or missing/expired challenge | Contact verified | POST consumes bound purpose once; GET previews only |
| forgot-password | `/forgot-password` | Address form | Generic delivery-pending response | POST; origin/login CSRF and abuse budget; no contact enumeration |
| reset-password | `/reset-password` | New secret form or expired challenge | Reset complete; prior sessions revoked | POST; purpose-bound single-use challenge and normalized password policy |
| auth-callback | `/oauth/{provider}/callback` | Pending provider proof | Verified method continuation | Protocol callback validation; not arbitrary GET action; state/nonce/PKCE/exact redirect |
| magic-link | `/auth/magic-link` | Browser-bound request/confirmation | Verified method continuation | Request/consume POST; purpose-bound challenge; GET preview does not authenticate |
| passkeys | `/account/passkeys` | No enrolled keys | Enrollment/removal acknowledged | Current principal; operation-bound proof and maintained passkey validation |
| mfa | `/account/mfa` | No enrolled factors | Enrollment/removal acknowledged | Fresh required proof; current org policy; no secret echo |
| auth-proof | `/auth/challenge` | Required operation proof prompt | Resume exact bound operation | Single-use expiring principal/input-bound proof; no local approved flag |
| recovery | `/auth/recovery` | Recovery method prompt | Recovery transition acknowledged | Current recovery policy and security epoch transition; no fallback exemption |
| profile | `/account/profile` | Current safe profile fields | Profile update acknowledged | Current person; POST origin+CSRF; verified contact transition separate |
| account-admin | `/account/administration` | Safe account state | Requested lifecycle transition | Current owner/admin authority and fresh bound proof; org role is not installation admin |
| workspace-chooser | `/workspaces` | No admitted organizations; own personal workspace | Authorized selection | Current server ownership/membership lookup; selector is untrusted |
| organization-create | `/workspaces/new` | New organization form | Active org with atomic creator membership | POST current person + explicit create policy/abuse checks |
| members | `/workspaces/{id}/members` | Empty filtered directory | Mutation/delivery pending or confirmed | Exact workspace policy; fresh proof for mutations; owner management separate |
| invitations | `/workspaces/{id}/invitations` | No outstanding invitations | Delivery/acceptance pending or confirmed | Current inviter permissions; verified recipient; committed capacity; atomic consume |
| invitation-confirm | `/workspaces/invitations/accept` | Bound confirmation or expired invitation | Admitted membership | POST; exact verified recipient; current org auth and seat policy |
| organization-auth | `/workspaces/{id}/authentication` | Explicit current policy/connections | Policy change acknowledged | Owner authority, enterprise entitlement, fresh policy proof, tested recovery path |
| billing-catalog | `/billing/catalog` | No selected product | Checkout intent pending | Selected workspace; server catalog; POST billing authority/current proof |
| billing-summary | `/billing` | No active subscription | Reconciled financial summary | Exact workspace billing.read; no checkout-redirect entitlement |
| billing-action | `/billing/payment-action` | Provider action required/expired | Reconciliation pending or confirmed | Billing.manage current proof; provider-bound action; unknown stays pending |
| billing-manage | `/billing/manage` | Current safe subscription controls | Confirmed transition or pending reconciliation | POST; exact workspace authority; durable idempotency; no automatic refund |

All listed pages inherit typed invalid-input, dependency-unavailable, denied and proof-required handling as applicable. Denied responses never reveal foreign workspace existence; an empty collection is not a substitute for denial. Theme/page replacement tests cover this matrix, including empty invitation/key lists and outage/loading behavior rather than only populated success fixtures.

## Public presentation seams

The public `ui` package owns exported renderer, view-model and theme contracts. Generated applications import public packages, never `internal/`. A renderer accepts request context, a named versioned page, a validated immutable view model and response presentation options. Typed models expose only fields necessary to render the page, safe navigation destinations, field errors, request ID and operation/challenge references. They exclude password verifiers, cookie/session secrets, service keys, raw provider events and diagnostic payloads.

Rendering receives escaped text by default through Go html/template. Trusted HTML is an explicit narrow library-owned capability; developer override HTML is authored code and cannot promote user content to trusted markup. Theme tokens select documented color, spacing, typography and focus styles. Asset manifests use application-local versioned paths, declared content types and integrity/cache policy; no remote executable assets or unrestricted template filesystem reads are introduced by a theme setting.

A page override registers the exact page version it implements. Full replacement consumes public APIs and standard authentication/proof flows with the same cookie/origin/CSRF policies. Missing or incompatible required page versions fail startup or render a visible unavailable state rather than silently dropping challenges. Upgrade PRs preserve authored overrides and report incompatible contract revisions; regenerated defaults do not overwrite authored files.

## Actions and navigation

GET/HEAD render or query; they do not sign in, accept invitations, consume verification/reset/magic-link tokens, change billing, enroll credentials or sign out. Confirmation mutations use declared typed operations with server-derived principal and exact input. Unsafe cookie-authenticated actions require current session, same-origin allowlist and CSRF. HTMX sends the same tokens and does not bypass the operation runner. A resumable proof challenge is operation/input/principal-bound; a replacement UI renders the challenge rather than inventing a local approved state.

Authentication return destinations are allowlisted local relative paths normalized by the runtime. Reject external URLs, protocol-relative URLs, encoded ambiguous paths and routes containing credentials. A callback does not trust Referer or arbitrary forwarding headers as an origin. Redirects following successful mutations use 303; validation failures preserve safe non-secret fields and never echo passwords/tokens. Responses containing authentication/account/billing data use no-store and explicit content-type/nosniff policy; shared/static assets have separate public caching rules.

Normal requests return a complete document. HTMX requests may return declared fragments with stable targets and appropriate Vary behavior; missing/invalid HTMX metadata falls back to the complete document, not a different security policy. Authentication expiry and dependency outage produce explicit usable responses rather than fragmenting a sign-in page into an unrelated target. JavaScript enhances navigation; core sign-in and account mutations retain a standard form path.

## Accessibility and verification

Pages have descriptive titles, landmarks, semantic labels and visible keyboard focus. Field errors connect through accessible descriptions and a focused error summary; status/proof/pending changes use restrained live regions. Dialogs trap focus only while open and restore it when closed. The UI works at narrow mobile widths, 200% text zoom, keyboard-only navigation and reduced motion. Color alone does not communicate errors or billing state. Password paste/autofill and password managers remain enabled.

The view inventory is exercised with real browser tests for full-page and HTMX paths, keyboard navigation, redirects, token-preview nonconsumption, expired sessions, cross-workspace selectors, escaped business data and unavailable services. Replacement-page fixtures must render operation-bound proof challenges and preserve CSRF/error semantics. Screenshots, rendered mock states and synthetic providers do not qualify identity/payment methods or an enterprise release.

## Rejected design fixtures

- Reject a theme or replacement page that grants workspace/paid access from a local UI flag.
- Reject a GET signout, invitation acceptance or bearer-link consumption.
- Reject unescaped user content, reflected password values or external return URLs.
- Reject HTMX-specific authentication bypass or stale authorized fragments cached publicly.
- Reject an upgrade that replaces authored frontend files without merge/conflict handling.

Implementations bind these contracts to the independently reviewed identity, workspace, billing and runtime policies. UI replacement preserves policy and machine APIs without requiring AMOS-maintained visual design.
