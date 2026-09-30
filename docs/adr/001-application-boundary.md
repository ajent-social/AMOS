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
