# ADR 013: explicit local email development origins

Status: accepted for implementation. Contract amendment v1.4.

Local generated applications bind canonical loopback and can use HTTP according
to the initializer/configuration contract. Email verification and rendering had
previously required HTTPS unconditionally, preventing that explicit local flow.
Their configurations now accept `DevelopmentLoopback`. Only an explicitly enabled
configuration using `localhost`, `127.0.0.1` or `::1` may use HTTP. The flag itself
also rejects remote HTTPS hosts to prevent treating production as development.
Remote origins and the default configuration continue requiring HTTPS. Action
links must match the configured scheme and authority. No arbitrary proxy origin,
forwarding header or remote HTTP URL is accepted.

Application composition must derive this flag from validated development mode,
local-only configuration and a loopback listener. Email transport is a separate
capability: local capture must be explicitly selected and must never masquerade
as SES or qualify live delivery. Secrets/challenge material remain encrypted and
are never emitted to ordinary logs. Production origin/provider tests remain
separate gates.

## Review and obtained evidence

Independent source review found no blocker in origin binding, encrypted material
scope, retained-key rotation, bounded expiry pruning or fail-closed local policy.
The scoped email/material/verification/password-policy/auth-protection tests and
vet passed; lint reported zero issues. Real PostgreSQL tampered-expiry mutation
failed the expected material-disclosure test; restored race checks passed. These
checks qualify local implementation boundaries, not production email delivery.
