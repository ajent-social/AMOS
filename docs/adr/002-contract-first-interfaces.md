# ADR 002: OpenAPI-first business and agent interfaces

## Status

Accepted product direction

## Date

2026 09 30

## Context

Human and agent clients must reach consistent behavior, including separately implemented services.

## Decision

Define business contracts in OpenAPI with explicit policy/exposure metadata, generate typed Go transport and MCP tools, and keep business implementations outside generated files. Use maintained SDKs and qualify exact supported protocol/client profiles.

## Consequences

Explicit contracts require maintenance and compatibility checks; generation alone cannot establish authorization or interoperability.
