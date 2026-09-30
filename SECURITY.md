# Security policy

AMOS is under active development. No release series or response-time guarantee
is currently declared supported. The architecture, threat model, and control
matrix record intended boundaries and planned controls; they are not evidence
that those controls are implemented or that AMOS is secure or production-ready.

## Reporting a vulnerability

Please do not publish vulnerability details, exploit code, credentials, private
data, or identifying infrastructure information in a public issue or pull
request.

If GitHub private vulnerability reporting is enabled for the AMOS repository,
use the repository's **Security → Report a vulnerability** flow to send a
private advisory. Its availability has not been verified by this policy. If the
private reporting control is unavailable, open a public issue asking the
maintainers for a private reporting route; include no vulnerability details in
that issue. Do not send sensitive material until the private route is confirmed.

Include the affected commit or version, the affected component, concise
reproduction steps using synthetic data, expected security impact, and a
suggested mitigation if known. Do not include customer content, secrets, raw
logs, or private paths. Coordinate disclosure with maintainers before making
details public. This policy does not promise an acknowledgement or remediation
deadline.

## Supported versions

There are no released versions with a declared support window yet. For active
development, report issues against the commit under review and verify any fix
against its recorded tests. A passing test suite is limited evidence and does
not establish security maturity.
