# ADR046: explicit development HTTPS testing

Status: proposed; independent design landing precedes source.

Adopt [v1.32](../contracts/development-tls.md) as a separate typed host constructor
and reference development executable for owner-requested LAN testing. Keep all
existing HTTP-loopback/generated/deployment profiles unchanged. Require exact
private IPv4 binding, TLS1.3, secure sessions, normal Origin/CSRF and owner-private
captured mail. This is a bounded development surface, not a production constructor.

A separate executable makes the end-to-end test path concrete without silently
reinterpreting generated runtime manifests or widening cloud/initializer modes.
Actual PostgreSQL/TLS lifecycle and certificate-verifying LAN tests precede any
usable-runtime claim. Browser trust, live providers and E16 remain separate.
