# ADR 001: Complete owner-hosted application with independent clients

## Status

Accepted product direction

## Date

2026 09 30

## Context

Applications need a complete paid foundation without coupling customer access to a specific agent runtime.

## Decision

Ship a complete Go SSR/HTMX application and developer CLI with integrated or privately proxied business features at one public domain. REST and standard generated MCP expose application behavior. Owners host their own installations. External agents and governance run independently. End-user CLI/mobile/SMS clients are later scope.

## Consequences

AMOS must enforce application permissions consistently while avoiding an application-level agent-governance system. Proxy trust and same-domain behavior require dedicated qualification.

## Boundary clarification (2026-10-03)

AMOS owns application identity, workspaces, policy, audit, billing and API/MCP.
It remains useful during external workflow outages. A code-change lifecycle
service owns PR safety and executable author/review/fix/re-review/landing tasks;
a product workflow controller owns product delivery and qualification. AMOS may
perform owner-local diagnosis, proposal/upgrade generation and bounded domain
jobs, but does not duplicate task/PR orchestration. This allocates roles and
does not claim adapter implementation or runtime qualification.
