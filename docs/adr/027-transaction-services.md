# ADR 027: Additive transaction-only service constructors

Status: accepted as a source contract through independently reviewed PR30, landed at `de3461086262d2c68a7f7bb770c8858f92a30bab`. No production qualification follows.
Date: 2026-10-07.

Existing constructors require development storage; substituting interface types would break function-value and Config compatibility. Additive constructors preserve those surfaces while reducing retained capabilities.

The exact decision, compatibility limits and required evidence are in the
[transaction-services contract](../contracts/transaction-services.md). No migration, provider
qualification or production deployment follows. Coordinator owns adoption and
shared wiring; bounded leaf authors require distinct reviewers.
