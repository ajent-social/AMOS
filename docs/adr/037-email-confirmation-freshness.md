# ADR 037: Consume email confirmation after its row waits

Status: proposed; independent review and guarded landing precede source.

Email confirmation has an inline transaction-start expiry check separate from
the shared challenge consumer. Merely adopting that consumer at the start would
still leave later contact/person waits before the final confirmation mutation.
The [v1.23 proposal](../contracts/email-confirmation-freshness.md) locks the exact
challenge, stages the guarded bound-contact/person updates, then uses the shared
post-lock consumer in the same transaction. Failure rolls back all staged writes;
success becomes public only after commit.

This reuses the adopted shared primitive instead of duplicating expiry SQL.
Sampling time before the row waits cannot satisfy the requirement. A global
writer-order redesign would be broader and remains separately gated; this change
preserves the existing challenge-first chain and claims no such qualification.

The source lane is restricted to Confirm and its new actual-service regressions.
No provider effects, new authority API, schema changes or product acceptance
follow from the proposal.
