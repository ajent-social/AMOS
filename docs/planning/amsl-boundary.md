# AMSL adoption and improvement strategy

Status: source-informed proposal; no AMOS consumer qualification or upstream change is claimed. Read [ADR 006](../adr/006-amsl-consumer-boundary.md).

## Responsibility

AMOS should compose qualified shared capabilities while serving as a concrete consumer that improves them. AMSL is a library/catalog/component ecosystem, not a required online service. AMOS remains the SaaS application foundation. Maintain independently testable boundaries and pin reviewed versions.

| Public candidate | Reuse opportunity | AMOS responsibility / qualification gap |
|---|---|---|
| `go/servicecred` | Issue-once secrets, expiry, scoped binding and revocation | Current user/membership/resource checks, key-management UI/API, lost-response behavior and paid admission |
| `go/mcpoauth` | OAuth lifecycle for MCP | Actual supported protocol/client profile, issuer/storage isolation, transactional policy checks, registration bounds and restore invalidation |
| `go/accounts` | Subject registry | Workspace ownership, membership, identity linking, lifecycle and recovery policy |
| `go/oidc`, provider adapters | Existing protocol integration | PKCE/nonce/callback composition, provider assurance, organization SSO lifecycle and adversarial consumer checks |
| `go/magiclink`, `passkey`, `passwordreset` | Qualified challenge mechanisms | Password authentication is a separate requirement; sessions, recovery, step-up and MFA enforcement need explicit composition |
| `go/billing/*` | Checkout intents, provider adapters, verified inbox and projection building blocks | Workspace ownership, multi-model billing, provider-authoritative reconciliation, plan transitions and freshness policy |
| `go/usage` | Bounded counters | Financial event ledger, deduplication/corrections/periods, billable units and provider reconciliation |
| `go/tenant` | Hosted instance lifecycle | Not organization membership or SaaS tenant authorization |
| `go/sealedvault` | Caller-sealed ciphertext persistence | Not encryption/KMS, rotation or a customer-key management service |
| `pulumi/aws/*` | Network, deployment identity, private database, container deployment and edge components | Selected compute/database/cost profile, component composition, live isolation, backup and restore evidence |
| `pulumi/cloudflare/dnsalias` | DNS-only alias and certificate validation records | Proxied mode is rejected by the current contract; choose and qualify a separate proxy contract if required |
| `workflows` | Reusable validation, release, container and preview jobs | Actual event trust, runner/builder choices, owner-controlled Pulumi state, caller check names and privileged credential boundaries |

## Evidence ladder

Source and docs -> focused package tests -> storage/concurrency/denied-path tests -> real AMOS composition -> provider/cloud/client verification -> compatibility experience -> explicit upstream maturity decision.

Each step establishes a different fact. Existing documentation calls these implementations candidates. AMOS adoption must not silently promote the catalog or replace missing upstream maintainer review. A public independently reproducible example is stronger evidence than an internal report.

## Upstream improvement workflow

1. Capture a concrete failing behavior using synthetic, publishable inputs.
2. Decide whether the missing guarantee belongs to an existing shared capability or to AMOS policy/composition.
3. Compare standard-library and mature external solutions before adding a new AMSL abstraction.
4. Propose the smallest contract change and compatibility/migration evidence.
5. Obtain required upstream review, release and public provenance clearance.
6. Pin the resulting artifact in AMOS and rerun the real consumer checks.
7. Record actual evidence; retain candidate maturity when its promotion gates remain unmet.

Upstream work never grants permission to modify a neighbor's active branch. Follow each repository's ownership and review rules. AMOS's owner-authorized autonomous maintenance policy does not override upstream security-sensitive merge controls.

## Public sources

- [AMSL bootstrap RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md)
- [AMSL Go](https://github.com/ajent-social/go)
- [Identity composition qualification plan](https://github.com/ajent-social/go/blob/main/docs/identity-composition-plan.md)
- [AMSL Pulumi components](https://github.com/ajent-social/pulumi)
- [Cloudflare DNS-only contract](https://github.com/ajent-social/pulumi/blob/main/contracts/cloudflare-dns-alias.md)
- [AMSL delivery workflows](https://github.com/ajent-social/workflows)

This planning pass inspected local source copies corresponding to these public projects. It did not verify remote freshness, run their tests, or establish deployed behavior. The implementation qualification tasks must select immutable versions and repeat the relevant checks.
