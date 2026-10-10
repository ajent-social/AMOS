# MCP client and authorization profile

Status: bounded design profile; AMOS interoperability is **unqualified**. This profile does not expose an MCP endpoint or authorize provider access.

## Immutable standards and implementation references

- MCP protocol revision: `2026-07-28`, official specification release commit `5f5440bb26a62e2cf3440b92da5a667efa03b267` ([release](https://github.com/modelcontextprotocol/modelcontextprotocol/releases/tag/2026-07-28)). The Go SDK release identifies specification source snapshot `f817239f4d6b1efff2c4dfc2f7af85c985d73076` ([SDK release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)).
- Go MCP SDK: `github.com/modelcontextprotocol/go-sdk` v1.7.0, source commit `bc72835f62eb94d0fb484439f886b6885b075f36` ([release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)); release declares full support for the protocol revision above and Go `1.25.0` minimum.
- TypeScript client: `@modelcontextprotocol/client` v2.0.0, source commit `cc4b41617ce3601b1290d67216ea0b194a3cd9ac` ([release](https://github.com/modelcontextprotocol/typescript-sdk/releases/tag/%40modelcontextprotocol%2Fclient%402.0.0)). It is a second, independently implemented target for future synthetic qualification.

These references identify candidate test targets; package support and version pins do not qualify AMOS interoperability. Two synthetic client scripts are required before qualification: (1) initialize, list tools, invoke an eligible read-only tool with a bound resource and valid issuer token; (2) attempt missing-version initialization, wrong audience, unregistered redirect, unsupported capability, and unauthorized side-effect tool calls. Record client version/commit, server build digest, protocol negotiation, request IDs, expected/actual status and stable denial code, and sanitized logs. No scripts have been run and no client is currently qualified.

## Authority and transport profile

The selected future transport is Streamable HTTP over TLS. A public client uses OAuth authorization-code with PKCE against a pre-registered client and exact HTTPS redirect URI; issuer discovery is allowed only for the configured trusted issuer. Tokens are accepted only when signature, issuer, expiry and exact protected-resource audience match. The resource identifier is the configured canonical AMOS resource origin and path; it is not inferred from a request header or token-selected URL. Each tool invocation still enters the ordinary AMOS app handler and operation policy. MCP eligibility metadata does not grant authority; a callable policy evaluator and composed adapter remain integration gates.

Public-client PKCE is the selected design path. Confidential-client support is outside this profile and unqualified. Protected-resource metadata and authorization-server discovery are restricted to preconfigured issuer/resource locations with issuer equality checks. Client-initiated metadata (CIMD) and remote client metadata fetching are disabled pending an explicit SSRF contract. Dynamic client registration is excluded. Redirects must exactly match a registered HTTPS URI; loopback redirects are excluded from this deployment profile. Only tools mapped from explicitly MCP-eligible operations are exposed. Prompts, resources, sampling, roots, elicitation, tasks, and other capabilities are disabled unless a later profile version explicitly enables and tests them. The profile is an AMOS subset, not full MCP interoperability.

## AMSL dependency and qualification gate

AMOS dependency evidence records the public `ajent-social/capabilities` RFC as architectural evidence only: no immutable implementation revision is selected, no AMSL source is imported, and candidate contracts remain subject to consumer qualification. See [`T1.4`](../tasks/T1.4.md), [`reuse-matrix.md`](../reuse-matrix.md), and [`provenance.md`](../provenance.md). Therefore there is no pinned AMSL contract against which to claim parity for PKCE, confidential clients, metadata documents, discovery, resource binding or redirects. This profile records those decisions and gaps directly; it does not invent an AMSL pin or claim AMSL/full interoperability. T11.1 remains gated until the integrator selects a pinned AMSL revision and completes the required comparison and synthetic client qualification.

## Machine-readable fixture

The accompanying fixture test parses this block. Unknown versions, clients, issuers and enabled capabilities fail closed.

```json
{
  "protocolVersion": "2026-07-28",
  "transport": "streamable-http",
  "resource": "https://amos.invalid/mcp",
  "issuer": "https://identity.invalid/issuer",
  "client": "registered-public-pkce",
  "enabledCapabilities": ["tools"],
  "disabledCapabilities": ["prompts", "resources", "sampling", "roots", "elicitation", "tasks", "client-metadata-fetch", "dynamic-registration"],
  "amslRevision": null
}
```
