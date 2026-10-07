# ADR 032: qualify session time after lock waits

Status: proposed; independent exact-head review and landing precede source.

A session renewal based on transaction-start time can erase an idle-expiry
boundary while waiting for a row lock. Later rechecks cannot reconstruct that
boundary. The same old clock can retain already-expired elevated assurance.

Adopt the bounded [v1.21 proposal](../contracts/session-renewal-freshness.md):
lock the scoped session without renewal, then use a fresh database instant for
old-expiry validation and renewal. Require READ COMMITTED explicitly and sample
assurance freshness after the held session lock. Preserve caller transaction
ownership, public method shapes and safe errors.

This is a freshness prerequisite, not operation authority. Opaque provenance,
refreshed-principal return, compatible complete writer lock graphs and challenge
producer freshness remain separately reviewed implementation gates. Source and
actual two-connection negative/restored tests need a different reviewer.
