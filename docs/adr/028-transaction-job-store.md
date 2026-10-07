# ADR 028: Transaction-only durable job storage

Status: proposed pending independent exact-head review and merge.
Date: 2026-10-07.

RuntimeDB exposes no raw pool. Add a compatible transaction-only constructor and post-lock database-clock fencing without adding connections or changing durable schema.

The exact decision, compatibility limits and required evidence are in the
[transaction-job-store contract](../contracts/transaction-job-store.md). No migration, provider
qualification or production deployment follows. Coordinator owns adoption and
shared wiring; bounded leaf authors require distinct reviewers.
