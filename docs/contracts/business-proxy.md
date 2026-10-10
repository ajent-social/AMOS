# Private business proxy profile

Status: design profile only. No upstream has been provisioned, no network origin or key has been qualified, and this document does not enable forwarding.

## Trust boundary and identity

A future proxy may connect only to a statically configured private origin selected by deployment profile. The origin must resolve to private addresses, use TLS with certificate and server-name validation, and authenticate the proxy using a dedicated client certificate. Trust bootstrap uses a deployment-bound reference to a pinned private CA bundle; roots are not request supplied. Redirects are rejected. DNS is resolved at startup and before each new connection; every resolved address is checked against the configured private CIDR set, and the connection is pinned to an approved result to prevent rebinding. Startup fails on missing/invalid origin, trust roots, client identity, or audience. The request cannot choose or override origin, host, path prefix, TLS identity or audience.

After AMOS authenticates a person and resolves current workspace selection and policy, the proxy may create a short-lived signed identity envelope for the fixed upstream audience. It contains issuer, audience, subject reference, installation/application/environment references, workspace reference, authorization decision reference, issue/expiry times and unique token ID. Expiry is at most 60 seconds; signing keys are held by the runtime secret facility and rotated by key ID with a maximum 24-hour overlap. Old keys are removed after overlap; new key IDs are distributed only through the deployment trust channel. Keys are never supplied by the request. The upstream must validate signature, trusted issuer, exact audience, expiry and replay policy. Unsigned, caller-provided identity headers/claims are stripped and never forwarded as authority. Only the person identity path is in scope; machine identities remain excluded.

## Request and response rules

The deployment profile supplies one origin and an explicit canonical path-prefix map. Only allowlisted business routes are forwarded. Canonicalization rejects encoded separators, dot segments, duplicate/ambiguous encodings and invalid UTF-8; query keys are bounded and duplicate policy is route-specific. Proxy-owned hop-by-hop, forwarding, authorization and identity headers are removed. The proxy adds only server-generated request ID and signed identity envelope. Cookies, arbitrary authorization headers, client host, and client-supplied identity are never forwarded.

Each request has a 10-second upstream timeout, 1 MiB request-body limit and 2 MiB response-body limit. Streaming and upgrades are disabled. Responses are buffered, content type is allowlisted, and internal headers are stripped. Retries are disabled for non-idempotent methods; only explicitly idempotent GET/HEAD may retry once before response headers, with the same request ID. There is no retry after partial response. Upstream failure maps to a bounded 502/503 response with a stable safe code. Internal/admin, identity, billing, health and readiness paths are excluded. The selected upstream must not redirect; any 3xx is returned as a safe failure, never followed.

## Current dependency boundary

T12.1's accepted extension contract owns route registration and operation policy; it does not provide a forwarding runtime or permission to bypass the application handler. T8.1 records AWS reference profiles but no provider resources were qualified. T8.2 distinguishes local profile/SSO from CI OIDC and keeps secret values out of configuration; no AWS or Cloudflare permissions were qualified. This profile therefore declares trust requirements without claiming a live private origin, authenticated channel, signing service, provider identity, key rotation, or deployed behavior.

## Machine-readable fixture

The accompanying fixture test validates the design profile and rejected variants. It is not an integration test of DNS, TLS, identity issuance, or routing.

```json
{
  "origin": "https://business.internal.invalid",
  "reachability": "private-only",
  "tls": "verified-mutual-tls",
  "trustBootstrap": "deployment-pinned-private-ca-reference",
  "clientIdentity": "deployment-bound-reference",
  "keyRotation": "key-id-with-24h-bounded-overlap",
  "issuer": "https://amos.invalid/proxy-issuer",
  "audience": "business-proxy-v1",
  "identity": "server-signed-short-lived-person-and-workspace",
  "maxTokenSeconds": 60,
  "targetSource": "deployment-profile",
  "requestTargetOverride": false,
  "redirects": "reject",
  "streaming": false,
  "internalRoutes": "deny"
}
```
