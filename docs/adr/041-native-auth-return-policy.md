# ADR 041: configure native sign-in return destinations explicitly

Status: proposed; independently reviewed design landing precedes source.

The native UI hardcodes a reference-business default and allowlist. A consumer
with its own protected account route must currently introduce a todos redirect
alias. Adopt the additive [v1.27 contract](../contracts/auth-return-policy.md):
validated exact local destinations, a configured default and constructor-owned
copies. Untrusted next values select only from this fixed set and otherwise use
the default. Nil configuration preserves current behavior.

Destination routing is presentation/navigation configuration; it creates no
resource authority and cannot claim reserved account/API roots. The destination
handler remains responsible for native session and current resource checks.
This is a supplemental T6.3 repair, separate from production-host and organization
acceptance. Historical task acceptance is preserved.
