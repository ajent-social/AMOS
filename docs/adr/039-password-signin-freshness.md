# ADR 039: bind password verification to current issuance rows

Status: proposed; independent review and landing required before supplemental
source. [The v1.25 contract](../contracts/password-signin-freshness.md) retains
one password verification and optional rehash, then locks/revalidates the exact
credential, contact and person in the caller transaction which stages issuance.

This addresses a concrete same-epoch changed-password/contact gap without
adopting the proposed database-wide writer gate or introducing a new API.
Existing baseline component acceptance is preserved. Supplemental source work
has its own six-stage lifecycle, claim and exact evidence. Whole-writer ordering,
complete producer finalization, current resource authority and host acceptance
remain open; deadlock failures are unavailable, not liveness qualification.
