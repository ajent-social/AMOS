# ADR 029: Finite callback SQL and lifetime guard

Status: accepted as a source contract through independently reviewed PR30, landed at `de3461086262d2c68a7f7bb770c8858f92a30bab`. No production qualification follows.
Date: 2026-10-07.

The abstract callback boundary needs exact SQL admission, concurrent-use and invalidation rules. Adopt a conservative SQL subset and cancel/drain ownership rules; trusted callbacks remain outside any SQL sandbox guarantee.

The exact decision, compatibility limits and required evidence are in the
[callback-database contract](../contracts/callback-database.md). No migration, provider
qualification or production deployment follows. Coordinator owns adoption and
shared wiring; bounded leaf authors require distinct reviewers.
