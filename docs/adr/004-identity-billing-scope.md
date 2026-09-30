# ADR 004: Full identity and separate workspace subscriptions

## Status

Accepted product direction

## Date

2026 09 30

## Context

The application foundation must serve individuals and organizations and expose paid functionality to humans and agents.

## Decision

Support the selected password/magic/social/passkey/OIDC/MFA methods, workspace membership and roles, separate personal/organization billing, and flat/seat/usage models. Stripe is the first provider behind an adapter. Deliver in stages while retaining the complete data/security architecture.

## Consequences

A small first reference path does not remove later requirements. Account recovery, provider assurance and billing policy require explicit contract decisions before implementation.
