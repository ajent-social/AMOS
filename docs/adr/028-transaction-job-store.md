# ADR 028: Transaction-only durable job storage

Status: accepted as a source contract through independently reviewed PR30, landed at `de3461086262d2c68a7f7bb770c8858f92a30bab`. No production qualification follows.
Date: 2026-10-07.

RuntimeDB exposes no raw pool. Add a compatible transaction-only constructor and post-lock database-clock fencing without adding connections or changing durable schema.

The exact decision, compatibility limits and required evidence are in the
[transaction-job-store contract](../contracts/transaction-job-store.md). No migration, provider
qualification or production deployment follows. Coordinator owns adoption and
shared wiring; bounded leaf authors require distinct reviewers.
