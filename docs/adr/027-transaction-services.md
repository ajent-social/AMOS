# ADR 027: Additive transaction-only service constructors

Status: proposed pending independent exact-head review and merge.
Date: 2026-10-07.

Existing constructors require development storage; substituting interface types would break function-value and Config compatibility. Additive constructors preserve those surfaces while reducing retained capabilities.

The exact decision, compatibility limits and required evidence are in the
[transaction-services contract](../contracts/transaction-services.md). No migration, provider
qualification or production deployment follows. Coordinator owns adoption and
shared wiring; bounded leaf authors require distinct reviewers.
