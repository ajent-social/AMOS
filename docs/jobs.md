# Durable job and outbox seam

`jobs.Intent` is durable work intent. `sqlstore.Store.EnqueueTx` inserts it into the caller's PostgreSQL transaction, so domain state and intent commit or roll back together. The job ID is server-generated UUIDv7; callers supply an installation/application scope, idempotency key, job kind, JSON payload, external-effect classification, and deadline. Limits are capped at 1 MiB per payload, 20 execution attempts, 100 reconciliation attempts, a 24-hour lease, and a 7-day retry delay. Reusing a scoped idempotency key with an identical request (including deadline) returns the original job; a different request hash returns a conflict.

`Store.Claim` uses database time and `FOR UPDATE SKIP LOCKED` to coordinate workers. Each claim increments a monotonic fencing token, records its owner and lease expiry, and chooses either execution or reconciliation. `Store.Resolve` compares owner, token, action, and unexpired lease; an expired or superseded worker cannot commit local state. Safe jobs whose worker disappears can be retried within their attempt budget. An external-effect job whose lease expires becomes `unknown`; a consumer must reconcile its provider outcome before another execution can be claimed. Exhausted reconciliation remains `unknown` for manual review rather than being marked as a known failure.

`jobs.Worker` is a narrow consumer seam. Consumers implement separate `Execute` and `Reconcile` operations and receive the immutable job UUID as the provider idempotency key. A lost provider response returns `ResolutionUnknown`; it never authorizes an automatic replay. Provider idempotency and reconciliation reduce duplicate effects but do not provide exactly-once remote execution. An operator or domain reconciler must resolve unknown outcomes using provider records before any non-idempotent retry.

Consumer guidance:

- Email delivery uses the job ID as a provider idempotency key where supported. Unknown delivery is reconciled against provider status or retained for human review; it is not resent blindly.
- Billing changes use the provider's idempotency mechanism and reconcile invoices/subscriptions before retrying an unknown result.
- Lifecycle transitions store desired state and deduplicate by job ID plus aggregate revision. Reconciliation compares current durable state and applies only a still-current transition.

`migrations/fragments/jobs.sql` is a migration proposal. Root migration sequence assignment and registry wiring remain integrator-owned. This task does not claim provider qualification or end-to-end production worker wiring.

Forward repair is preferred. If this proposed schema must be rolled back before integration, stop consumers and preserve unknown records for reconciliation; do not automatically delete outbox rows or external-effect history. A destructive table removal requires an owner-reviewed retention decision and backup.
