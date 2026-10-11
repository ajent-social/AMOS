# Private service identity assertions

This package implements the signed identity envelope in the private business
proxy profile. It is a component boundary, not a configured or deployed proxy.

Assertions are compact JWTs signed with EdDSA using the repository-pinned
`go-jose` implementation. The signer holds one Ed25519 private key; verifiers
receive public keys by key ID only. Claims bind a person reference, installation,
application, environment, workspace, authorization decision, scopes, HTTP method
and canonical escaped path to the fixed issuer and audience in `identity.go`.
The signer limits assertion lifetime to 60 seconds. Verifiers reject expired
assertions, future issuance beyond five seconds, untrusted key IDs, mismatched
issuer/audience/request binding and invalid key overlap.

Verification requires an atomic `ReplayStore` shared by every verifier that can
reach the same upstream. The store must retain an issuer/token-ID pair through
expiry plus the configured clock-skew bound. A process-local map is not a valid
production implementation. Replay-store errors fail closed. During rotation,
old public keys may remain trusted for at most 24 hours after retirement; the
private key and public-key distribution remain deployment responsibilities.

`StripCallerIdentityHeaders` removes the identity headers AMOS recognizes at the
external boundary. `SetAssertionHeader` replaces them with the server-generated
assertion. `Middleware` consumes the assertion, removes it before the handler,
and exposes verified claims through `IdentityFromContext`.

This package does not load signing secrets, distribute verification keys, create
the mutual-TLS client identity, resolve or pin the private origin, or wire the
proxy into an application route. Those remain separate integration and
operational gates. The pinned `go-jose` v4.1.5 update includes upstream fixes for
JWT date bounds, Ed25519 key validation and per-signature verification:
[upstream release notes](https://github.com/go-jose/go-jose/releases/tag/v4.1.5).
