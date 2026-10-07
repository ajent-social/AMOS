# ADR 031: require preprovisioned runtime binding

Status: proposed; independent review and landing precede source work.

The development host initializes deployment binding with migration-capable
storage and may insert the singleton row. A production host must have no such
initialization authority. The existing immutable RuntimeBinding schema provides
a realm identity that can be checked with a read-only runtime transaction.

Adopt the bounded private helper in
[the v1.20 proposal](../contracts/runtime-host-readiness.md). Keep development
initialization unchanged, require preprovisioned binding, preserve failed
transaction completion and sanitize errors. A separate fixture operator owns
schema and synthetic binding setup; runtime tests use restricted TLS roles.

This decision does not allocate a migration, certify production grants, or
implement the full production host. Canonical HTTPS, secure cookies, explicit
proxy policy, service/key/provider capabilities, full schema readiness,
lifecycle and current-authority composition remain separate reviewed gates.
