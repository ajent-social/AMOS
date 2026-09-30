# Workspace and membership contract v1

Status: implementation contract pending independent review. Organization lifecycle and provider-backed admission remain staged capabilities, not implemented controls. This contract consumes the frozen identity and operation contracts.

## Scope and ownership

Every workspace and membership has an immutable server-generated UUIDv7 ID. Every query and uniqueness constraint includes installation and application scope. The authenticated environment is also checked at request/session/grant boundaries; resources from another environment are never selected merely because their ID matches. Person credentials and external identities remain distinct from workspace resources.

A personal workspace has exactly one owner person, no organization memberships, no invitations, and no ownership-transfer operation. There is exactly one personal workspace per installation/application/person. Creation is atomic with the person's bootstrap or an idempotent verified bootstrap transition; retries cannot allocate another workspace. Another person cannot acquire this workspace through a role, organization, email match, or account-administration privilege.

An organization has active, suspended, and deletion-pending lifecycle states. Its creator becomes an active owner atomically with creation. Active organizations require at least one active owner person. Organization labels/slugs are presentation values, not authority. Membership is unique for installation/application/workspace/person, with `active`, `suspended`, and `left` states. Rejoining is an explicit audited transition, not a new duplicate membership that bypasses prior restrictions. Identity disablement always takes precedence over an active membership row.

## Selection and policy

A session's preferred workspace, a path parameter, CLI flag, MCP argument, or request header is an untrusted selector. The server resolves it to a currently authorized workspace using the authenticated person's ownership or current active membership and the machine credential's current grant. Email domain, provider claims alone, or unchecked headers cannot select or grant an organization. A selector outside authority is denied without revealing whether the workspace exists. Unavailable membership/grant/entitlement state is unavailable, never a cached allow.

An operation requires an explicit workspace, actor kind, permission, entitlement requirements, and assurance. A person session alone is insufficient for organization access. Organization permission does not confer authority over that person's other personal workspace. Machine credentials may narrow current ownership/membership authority and cannot outlive their owner's revocation or membership. Business code gets the verified principal and declares its resource-specific permissions; it cannot reinterpret an ID/header as a principal.

## Built-in permissions

Roles are `owner`, `admin`, and `member`. Role meaning is versioned and defined by permissions, not ordinal comparisons. Custom business permissions register under an application namespace; AMOS-sensitive permissions are reserved. New permission registration does not silently grant it to every existing role. A business administrator role never becomes installation administrator.

| Operation | Actor and workspace | Authority | Entitlement | Fresh proof |
|---|---|---|---|---|
| Select personal workspace / read own profile | Person; own personal | Current active account and ownership | None | Normal session |
| Select organization / read permitted business resource | Person or permitted machine; organization | Current active membership/grant plus business permission | Operation declares exact feature requirements | Operation declares assurance |
| Create organization | Active person; new organization | Explicit organization-create permission | Installation product policy, not another workspace's subscription | Normal session; abuse controls |
| Read organization directory | Member, admin, owner | `workspace.members.read` | None unless product explicitly declares one | Normal session |
| Invite/remove/suspend a member | Admin or owner; organization | `workspace.members.manage`; admin cannot alter owners or elevate to owner | Admission/seat capacity from this organization only | Authentication within 15 minutes; organization MFA policy applies |
| Promote an owner / transfer ownership / alter owner role | Owner; organization | `workspace.owners.manage`, verified target and last-owner invariant | None | Fresh proof satisfying current organization policy |
| Change organization authentication policy / enterprise connection | Owner; organization | `workspace.authentication.manage`; preserve an approved recovery path | This organization's declared enterprise feature | Fresh MFA or stronger policy proof |
| Subscribe/manage/cancel personal subscription | Person; own personal | `billing.manage` from personal ownership | Billing state and selected product rules | Fresh proof for sensitive mutations |
| Subscribe/manage/cancel organization subscription | Owner; organization | `billing.manage` for this organization; admin/member roles do not imply it | This organization's billing state only | Fresh proof; organization MFA policy applies |
| Read subscription summary | Personal owner or organization admin/owner | `billing.read` in the selected workspace | None | Normal session |
| Delete organization | Owner; organization | `workspace.delete`, current ownership and dependency checks | Billing lifecycle must reconcile closure separately | Fresh proof; explicit confirmation bound to operation/input |
| Disable person as installation administrator | Separately enrolled installation administrator | Installation account-administration permission; never inferred from organization role | None | Fresh required administrative proof |

The first release does not introduce customer agent governance or an approval inbox. An independently governed agent uses the same permission, entitlement, and assurance rules. A machine cannot bypass required human proof; it receives the same resumable operation-bound challenge.

All role and person labels in the matrix also permit a machine with an explicit current grant derived from that authority, subject to operation exposure policy. Required human proof remains bound to the machine principal, exact operation/input, and current owner policy; a machine key or old grant is not that proof. Authentication-secret enrollment flows may remain proof challenges rather than accepting secrets through agent tools.

## Invitations and seats

Only an authorized inviter can create an invitation, and the requested role cannot exceed their allowed assignment authority. Invitations bind installation/application, organization, a canonical intended recipient, intended role, expiry, inviter, and purpose. Secrets are random, stored only as digests, and never logged. Initial expiry is seven days, owner policy may shorten it, and accepted/revoked/expired invitations cannot be replayed. A GET or email preview does not accept an invitation.

Acceptance requires an active authenticated person with a verified contact comparison key matching the intended recipient. It is not social-account linking. Recheck invitation status, inviter/organization policy, current recipient membership, authentication enforcement, and committed capacity in the same serialized acceptance transition. Consume and create/reactivate the membership atomically; simultaneous accepts produce one membership. If provider-backed capacity is not yet committed, return a pending/unavailable outcome or deny admission according to the explicit billing policy; do not grant access based on a checkout redirect.

The active roster counts active human memberships, including owners and admins. Pending invitations, suspended/left memberships, and disabled persons do not create active access seats. The billing domain explicitly determines how that roster maps to each subscription model's billable quantity; flat-price or usage plans must not accidentally become seat plans. Membership mutations and a durable seat-reconciliation intent commit together where applicable. Provider calls never run inside the membership transaction. A personal subscription never pays for an organization seat, and one organization's subscription never grants another organization's feature.

## Suspension, ownership, deletion, and authentication enforcement

Suspension removes organization business access while allowing only explicitly authorized management/recovery operations. Payment suspension, administrative suspension, and member suspension are distinct reasoned states. A role change, invitation revoke, membership suspension, or account disable invalidates derived grants/current policy on subsequent authorization; cached UI state cannot preserve access.

Ownership transfer locks the organization/owner roster, verifies an active admitted target, promotes the target and demotes the source atomically, and writes audit/outbox evidence in that transaction. Concurrent last-owner leave/delete/demotion operations cannot leave an active organization without an active owner. Ordinary self-disable must resolve last-owner responsibilities first. An emergency installation-administrative security disable may suspend the affected organization atomically when its last active owner is disabled; it does not promote the administrator or invent a replacement owner. Resuming requires an explicitly authorized, audited recovery/transfer procedure and current proof.

Organization deletion is an explicit lifecycle transition with retention/erasure and recovery policies, not a cascading SQL delete hidden inside a UI request. Active provider subscriptions, in-flight billing/exports, jobs, invites, keys/grants, and ownership must be reconciled. Deletion or recovery never resurrects revoked credentials or prior maintenance decisions. Retention periods and erasure evidence belong to the privacy and recovery tasks.

Enterprise OIDC admission and MFA enforcement are organization policies. A linked social account, password reset, magic link, or account recovery is not an exemption. Connection issuer/subject, allowed methods, proof level, and current membership are checked at every organization boundary. Policy changes cannot lock out the last owner without an approved tested recovery path, and unqualified/unavailable providers cannot be represented as working fallback methods.

## Design rejection fixtures and downstream checks

- Reject organization selection from an email suffix or unchecked `X-Workspace-ID`; a requested ID requires current server-side membership/grant lookup.
- Reject personal billing checkout used to satisfy an organization entitlement, even when the person owns both.
- Reject an admin inviting/promoting an owner without reserved owner-management authority.
- Reject simultaneous last-owner departures that leave an active organization ownerless.
- Reject invitation acceptance by an unverified or different recipient; no auto-linking by email.
- Reject organization access after membership/account suspension or while required policy state is unavailable.

These are required design outcomes, not runtime test results. T4.2 onward must exercise actual SQL transactions and HTTP/API/MCP authorization boundaries. Root API fragments must express this matrix through the common operation metadata and service runner.
