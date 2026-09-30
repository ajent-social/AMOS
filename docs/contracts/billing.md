# Billing and entitlement policy v1

This is the implementation policy for all three subscription models. It is not live-provider qualification. Each installation is the merchant for its own customers; AMOS does not aggregate customers, payment credentials or revenue into a shared service.

## Catalog and ownership

Every subscription binds installation, application, environment and exactly one personal or organization workspace. Catalog revisions are immutable, server-controlled and identify product, provider price, model, currency, billing interval, feature set and effective date. Public requests contain only a catalog selection; clients cannot supply amounts, provider price identifiers, seat prices, quantities, currency overrides or entitlement claims. Personal billing never pays for organization access. Organization billing never pays for another organization.

Amounts use integer currency minor units and explicit currency metadata, including zero-decimal currencies; never floating-point arithmetic. A workspace uses one active base subscription per product family, with explicit compatible add-ons. The catalog may combine a flat base, seats and metered usage only through declared components, with no double-counting implicit in a model change. Monthly and yearly are calendar intervals; a year is not 365 daily periods. Catalog changes do not silently migrate existing contracts or currencies.

Billing activation requires owner configuration of a real qualified provider, catalog, merchant/tax responsibility and supported currency. Missing financial configuration is unavailable, not a free successful checkout. Secrets are resolved from owner-controlled protected references.

## Seat model and membership admission

Billable roster entries are active human organization memberships, including owners and admins, whose persons are active. Pending invitations, suspended/left memberships and disabled accounts do not count. Personal workspaces have no organization seats. Flat-only and usage-only products do not acquire seat charging because a roster exists.

Seat subscriptions purchase committed capacity. Increases request the provider's immediate invoiced proration and grant additional admission only after authoritative payment/subscription reconciliation commits the increased capacity. Unknown or payment-action outcomes preserve the previous capacity. Acceptance of an invitation locks capacity and membership together, so concurrent admits cannot oversubscribe. Provider calls do not run inside that SQL transaction.

Reductions take effect next renewal and never lower capacity below the active roster. Membership removal changes authorization immediately; it does not invent an immediate refund. Owners can intentionally keep unused purchased capacity until renewal. A durable scoped roster/capacity reconciliation intent accompanies affected membership mutations. Roster quantity and committed financial capacity remain separate fields.

## Lifecycle and payment outcomes

Checkout return URLs are presentation only. A signed callback is verified, durably deduplicated, and reconciled against the provider's current scoped subscription state before granting access. Out-of-order callbacks cannot resurrect cancellation or supersede newer state. Explicit pending, requires-payment-action, active, trialing, past-due, suspended and terminal states are retained without collapsing ambiguity into success. Unknown results require reconciliation using a durable idempotency key; they do not trigger blind replay.

Default trials are disabled. An explicitly catalog-enabled trial has a fixed end and bounded abuse policy, with declared features and capacity; it does not grant paid features beyond its contract. Trial expiry follows authoritative state. Default delinquency grace is zero for features that explicitly require current paid status. An owner may configure a versioned bounded grace policy before publication; grace starts once at the first delinquency of that invoice and retries do not extend it. Active person sign-in, profile, recovery, export requests and authorized billing management remain accessible without paid-feature entitlement.

Cancellation defaults to period-end: entitled paid access continues to the authoritative paid-through boundary. Reactivation before that boundary is an explicit provider-backed transition. Cancellation after the boundary requires a new confirmed purchase; an old checkout or callback is not reactivation. Immediate administrative suspension removes paid business access without claiming that an external subscription was canceled. Billing cancellation and workspace deletion reconcile independently.

AMOS never issues automatic refunds from UI redirects or local account deletion. A refund operation requires explicit authorized amount, original charge/currency binding, current proof, durable idempotency and provider confirmation. Refund entitlement effects are declared in the product policy rather than guessed from amount. A dispute suspends affected paid features after current provider reconciliation; resolving it restores access only if the current subscription still qualifies. No financial history is erased to hide a dispute or refunded charge.

## Metered usage

Each accepted usage record has a scoped source event ID, declared metric, nonnegative integer quantity, occurrence timestamp and receipt timestamp. Sources are authenticated and authorized; end-user client quantity reports do not become charges without a trusted ingestion policy. Duplicate IDs with the same normalized input are idempotent; a conflicting input is rejected. A source may not debit another workspace.

Periods use UTC with half-open boundaries [start, end), aligned to the provider's authoritative subscription billing period. Occurrence time determines the original period; receipt time determines lateness. Default acceptance permits at most five minutes future skew and a 24-hour late-arrival window after period end. These bounds are versioned catalog settings, not hardcoded assumptions about provider capabilities. Backdating beyond the configured window is quarantined for explicit owner financial review, never silently invoiced into a different period.

Corrections append an authenticated adjustment referencing the original event; they never overwrite the ledger. Signed integer adjustments cannot drive an aggregate below zero. Before invoice finalization, supported corrections reconcile to the same period. After finalization, a correction requires an explicit credit/debit workflow tied to the original invoice, not mutation of the issued invoice. The adapter must prove the provider supports the declared late-arrival and correction semantics; unsupported configuration fails activation.

## Taxes, invoices and entitlements

The installation owner is responsible for merchant registration, tax configuration, invoice disclosures and financial/legal policy. The provider issues authoritative invoices and charge records. Provider-managed tax may be enabled only with the required merchant configuration; AMOS does not infer tax exemption or use a tax rate supplied by a client. Financial setup is an explicit installation release gate.

Entitlement evaluation returns allowed, denied or unavailable with scoped policy revision, authoritative observation time and validity boundary. Current database policy is evaluated on every protected operation; unavailable state never uses a stale allow. A positive provider-backed projection expires after five minutes without successful reconciliation by default, and earlier at paid-through/trial/grace boundaries. Provider callback processing immediately invalidates affected projections; periodic reconciliation supplements callbacks. Installation owner policy can shorten freshness but cannot extend a persisted lease accidentally during an outage.

Reconciliation budgets bound attempts, elapsed time and provider concurrency; exhaustion yields unavailable/manual intervention, not fabricated settled state. Account, recovery and authorized billing paths use their own permission checks and do not require paid-feature access. Enterprise/OIDC and workspace policy remain enforced independently of payment status.

## Rejected design fixtures

- Reject a personal checkout used to unlock an organization feature.
- Reject client-provided prices, quantities or tax exemption claims.
- Reject invitation admission based on a success redirect or uncommitted seat increase.
- Reject duplicate usage with changed quantity and silent late-period reassignment.
- Reject financial success after ambiguous provider timeout without reconciliation.
- Reject paid entitlement past its freshness or financial boundary during provider outage.
- Reject a refund, immediate cancellation or currency migration inferred from account deletion.

Runtime tasks must implement and exercise these boundaries against real SQL and qualified provider APIs. Catalog activation remains blocked until the installation's money-impacting settings and provider semantics are explicit.
