# Identity contract v1

Status: implementation contract, reviewed against `amos-contract-v1`; authentication is not yet implemented. Provider qualification and adversarial release gates remain required.

## Entities and trust

A person is an installation-local human account with an immutable UUIDv7 ID. An external identity is a separate immutable record keyed by provider connection, exact issuer, and verified subject; provider subject and email never become a person ID. A workspace is a resource and billing container with its own ID. Personal ownership and organization membership are authoritative database relationships, not authentication credentials. A machine credential belongs to an explicit owner and application/environment scope; it narrows authority and cannot impersonate a person. All IDs are server-generated and parsed through the shared ID contract.

Account states are `pending_verification`, `active`, `self_disabled`, `administratively_disabled`, and `deletion_pending`. Credentials can be verified while the account is inactive; inactive state still blocks session issuance and all authenticated actions. Pending verification permits only verification, resend, and signout operations. Signup and personal workspace creation are atomic; a retry cannot create a second personal workspace. Reactivation requires the appropriate state-specific flow, never a fresh sign-in alone.

Installation administrator is an operationally enrolled authority separate from organization owner. A normal signup, an email suffix, a first account, an organization role, or access to billing cannot create installation administrator authority. Self-disable requires the current person and fresh authentication; administrative disable requires a separately authenticated installation administrator with the explicit account-administration permission and fresh proof. Neither action silently transfers organizations or bypasses last-owner constraints. Recovery, deletion, revocation, and membership rules apply to administrators too.

## Email and linking

Accept one validated UTF-8 mailbox address, without display names, comments, control characters, or multiple recipients. Store the user-visible form separately from a comparison key. For v1, comparison uses a lower-case ASCII local part and a lower-case ASCII domain; non-ASCII mailbox/domain inputs are rejected with a clear validation error until an internationalized policy is qualified. This deliberately supports case-insensitive local-part comparison; it does not strip plus tags, dots, or provider-specific aliases. Apply this same key to signup, login, verification, reset, email change, and account linking. Uniqueness is scoped to installation/application, enforced in PostgreSQL, and never inferred across installations.

A verified email is a contact assertion, not evidence that two external identities are the same person. Social/enterprise callbacks never automatically link by email. Linking requires an active authenticated person with fresh proof, explicit intent bound to that session, and independent verification of the new credential. If another person already owns the external identity, reject without revealing their account. Removing a credential must preserve at least one usable required authentication method and satisfy organization authentication policy. Tenant OIDC is keyed by connection and exact issuer/subject; organization admission and identity creation are separate decisions.

## Password policy

Use a maintained Argon2id implementation. Initial parameters are 64 MiB memory, three iterations, one parallel lane, a cryptographically random 16-byte salt, and a 32-byte output. Store a versioned, bounded encoding containing algorithm, parameters, salt, and output; never store plaintext or reversible passwords. Parsing rejects unknown algorithms and parameters outside explicit resource bounds before hashing. Successful verification can upgrade older approved hashes atomically without lowering assurance. Parameters must be benchmarked on both supported deployment profiles before production qualification.

Passwords contain 15–128 Unicode code points and at most 512 UTF-8 bytes. Normalize consistently to NFC before counting and hashing; do not trim, silently truncate, or impose character-class composition rules. Compare against a local, versioned compromised/common-password blocklist with documented provenance; failure to load required policy data makes password enrollment unavailable. Never send plaintext passwords to a provider, model, telemetry, or upstream issue. Do not force periodic changes absent compromise. Permit paste and password managers. Limit concurrent expensive hashes and apply bounded rate limits before costly work; use a valid dummy hash for unknown accounts to avoid the simplest existence timing signal.

The minimum-length choice follows [NIST SP 800-63B-4](https://pages.nist.gov/800-63-4/sp800-63b.html); parameter and resource-bound choices are AMOS policy, informed by [OWASP password storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). This is not a NIST certification claim.

## Sessions and request boundaries

Issue at least 256 random bits per opaque session token; persist only a one-way digest. Bind the record to installation/application/environment, person, credential/authentication time, current revocation epoch, and expiry. Initial limits are a 12-hour absolute lifetime and a 30-minute idle lifetime; owner policy may shorten them. Rotate on login and successful assurance elevation; old tokens become unusable atomically. Every request checks active account, expiry, revocation, current membership and grants against the authoritative database. An unavailable authority source returns unavailable, never cached allow.

Production uses a `__Host-amos_session` cookie with Secure, HttpOnly, SameSite=Lax, Path=/, and no Domain attribute. Tokens never appear in URLs or local storage. Explicit loopback-only HTTP development uses a separately named development cookie; production configuration cannot select it. Signout revokes the server-side session and expires the browser cookie. Account disable, password reset, credential removal, and recovery invalidate affected sessions and refresh grants in the same durable security transition.

Unsafe cookie-authenticated browser operations require a session-bound CSRF token and an exact allowed Origin check (a validated same-origin Referer is the explicit fallback when Origin is absent). Signout is a mutation, not a GET side effect. Request-size limits, generic public errors, bounded login/reset attempts, and audit redaction apply to all transports. API keys and OAuth bearer credentials are explicit alternate authentication mechanisms; ambiguous credential combinations are rejected. API/MCP failures return typed authentication/authorization/proof errors and never an HTML login redirect.

## Proof, methods, and recovery

Confirmed methods are email/password, email magic link, Google, GitHub, Apple, passkey, and per-organization enterprise OIDC. Disabled/unqualified methods are visibly unavailable rather than simulated. Callback flows validate issuer/audience/signature, state, nonce where applicable, PKCE, exact registered redirects, transaction expiry, and single consumption. Secret callback parameters are not logged. Passkeys validate RP ID, origin, challenge, user verification, and counter/backup semantics through a maintained implementation.

Email verification tokens, reset tokens, magic-link tokens, and invitations are distinct purpose-bound records with random secrets stored as digests, bounded expiry, single consumption, and explicit recipient/account binding. A link preview or GET does not consume a token or issue a session; a confirmation mutation does. Magic-link flow requires an initiating browser-bound transaction by default and provides an explicit confirmation flow for a different device. Email contact verification is not a second authentication factor. Organization-enforced MFA cannot be bypassed by choosing another method or recovering a password.

For v1, fresh authentication expires after 15 minutes and is additionally invalidated by relevant security transitions. Sensitive actions require proof appropriate to organization and operation policy: password alone does not satisfy a second-factor requirement. Proof challenges are resumable, operation/input/principal-bound, expiring, and single-use. Recovery must meet current policy and cannot resurrect revoked credentials, grants, sessions, or maintenance approvals after database restore; the non-restored activation authority is specified by the recovery task. No security questions, support backdoor, or agent exemption is introduced.

## Required decision examples

| Input or transition | Required result |
|---|---|
| Same verified email from a new social provider | Separate unlinked identity; explicit authenticated linking required |
| Correct password for disabled account | No session; generic public failure |
| Pending person requests a paid business operation | Denied; verification allowance does not confer business access |
| Organization owner requests installation administration | Denied unless separately enrolled and explicitly permitted |
| Revoked machine key supplies a current workspace ID | Unauthenticated; workspace selection cannot restore authority |
| Required authoritative store unavailable | Unavailable; no stale allow |
| Two simultaneous signup requests with one comparison key | One person and personal workspace; the other request has a non-enumerating conflict outcome |
| Password reset for MFA-enforced organization member | Password reset alone does not satisfy MFA or organization admission |
| Existing identity owned by another person requested for linking | Denied without revealing that person |
| Canonical email containing plus tag or dots | Preserved in the comparison key; no provider alias collapsing |

These examples are design rejection fixtures, not evidence of working runtime controls. T3.2 onward must turn them into real database/HTTP tests. Full client/provider and recovery qualification remains in the staged release plan.
