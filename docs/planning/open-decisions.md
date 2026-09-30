# Decision and external-gate register

Status: unresolved implementation choices have a named freeze owner. Product choices already recorded in VISION and ADRs are not reopened here. Proposed defaults are planning recommendations, not hidden approvals. Work that does not depend on a choice can proceed.

| Decision | Proposed baseline / evidence needed | Freeze / affected work |
|---|---|---|
| Original-work license and import rights | Select an open-source license, inspect individual source/third-party rights, publish only cleared evidence and notices | T1.4/T1.7; uncleared copying blocked |
| Exact dependency/toolchain/generator versions | Official maintained releases and immutable pins; qualify rather than assume local source equals a release | T1.1/T1.4/T2.1 |
| Session/password parameters and revocation bound | Maintained primitives, durable opaque sessions and current-state checks; benchmark safe hashing and define key handling | T3.1/T3.4 |
| Installation administrator authority | Explicit owner provisioning separate from tenant roles; audited least privilege | T3.1/T4.1 |
| MFA/passkey assurance and recovery | TOTP, qualified passkey assurance, single-use recovery and limited recovery states; no email-string or operator bypass | T3.1/T3.15-T3.17/T4.11 |
| Enterprise organization enrollment | Verified configuration and preprovisioned membership initially; domain verification alone grants nothing | T4.1/T4.9-T4.10 |
| Pricing combinations and financial policy | Explicit catalog/seat counts, period rules, proration, usage units/corrections, tax responsibility, trial/grace/refund/dispute semantics | T5.1; money-impacting behavior blocked until frozen |
| Email provider and delivery policy | AWS SES candidate, durable outbox, sender verification, suppression/feedback and bounded retries | T1.11/T8.27-T8.28 |
| Cloudflare mode | Required DNS integration first; proxied mode has separate origin/TLS/WAF consequences and qualification | T8.8/T8.16 |
| Concrete AWS implementations and operating budgets | ECS/Fargate plus RDS and EC2 plus Podman are candidates for the confirmed two-profile requirement | T8.1/T8.2; owner account/region/budget gates |
| API/MCP version and client support | Pin actual specification/SDK/profile and qualify real independent clients | T11.1/T11.8/T11.14 |
| Separate-service trust mechanism | Maintained signed assertion or mutual TLS, exact audience/scope/routing and rotation; no trust in client-supplied identity headers | T12.4/T2.9 |
| Recovery objectives and data retention | Per-profile RPO/RTO targets chosen by owner, measured drills, isolated restores, explicit authority activation barrier | T10.1/T10.10/T10.11 |
| UI accessibility and override support | Versioned view/action contracts, keyboard/responsive behavior and sampled assistive checks; publish measured coverage | T6.1/T6.16 |
| Autonomous maintenance defaults | Disabled until owner configures policy/credentials/budget; then permitted classes may auto-promote through independent gates | T13.1/T13.2/T13.9 |
| Diagnostic privacy and retention | Allowlisted bounded public metadata, no business frames/raw messages, local preview/disablement and separate vulnerability channel | T14.1/T14.7 |
| Upstream hosting and maintainer identities | Separate account/service contract; no access to consumer private repositories or deployment credentials | T14.13/T14.14 |

## External gates

The execution task records exact external requirements: owner AWS/Cloudflare/domain access, GitHub repository/App installation, email sender/recipient authorization and provider review, payment test catalog and mode, identity registrations/device qualification, model access/spend limits, release authority and upstream review. Calendar assumptions never resolve these gates.

Provider prerequisites must be checked through authorized capabilities; do not store credentials in this register. Live checks record sanitized outcomes and explicit NOT_RUN/BLOCKED states. Preparation, fixture checks and a reviewable artifact should precede any additional owner approval request.
