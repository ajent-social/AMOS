# Durable billing webhook job composition: 2026-10-01

Frozen source `b9311ea` plus final lease correction `daaa4dd` composes verified
ingress with the durable billing-only jobs consumer. Independent headless
reviews found no remaining scoped source blocker, including the final
post-insert database-clock lease guard.

## Obtained evidence

- Worker real PostgreSQL race tests cover billing-only claims, strict bounded
  envelopes, current customer/workspace scope, duplicate-link preservation,
  unknown-customer later scanning and expired leases.
- The final receipt-link regression blocks INSERT with a real PostgreSQL table
  lock past lease expiry and verifies dirty-work and link writes roll back.
  Removing the final guard makes it fail; restored scoped race passes in
  7.749 seconds, vet passes and pinned lint reports zero issues.

- Fresh integrator race tests of the full reconciliation package passed in
  7.535 seconds against required PostgreSQL, with scoped vet, zero-issue lint,
  planning and public-artifact scans passing.

## Limits

T5.8 remains in progress. This component does not establish executable
scheduling, globally allocated migration ordering, account-wide throttling,
complete subscription/access behavior or live Stripe qualification. Fixtures
do not qualify live providers. No deployment or complete release is claimed.
