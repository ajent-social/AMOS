# Dependency and ownership waves

Waves are a capacity ceiling, not authorization to dispatch blocked work. Each listed task has its own owning lane and isolated workspace. Skip externally blocked tasks; do not skip their dependencies. An actual runtime with fewer slots runs a compatible subset. A lane has at most one active writer.

## Wave 1: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.1](../tasks/T1.1.md) | L01 | S0 | None |
| [T1.4](../tasks/T1.4.md) | L02 | S0 | Owner chooses original-work license and clears any restricted-source import before copying. |

## Wave 2: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.2](../tasks/T1.2.md) | L01 | S0 | None |
| [T1.6](../tasks/T1.6.md) | L02 | S0 | None |

## Wave 3: 8 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.3](../tasks/T1.3.md) | L01 | S0 | None |
| [T1.5](../tasks/T1.5.md) | L02 | S0 | None |
| [T3.1](../tasks/T3.1.md) | L03 | S1 | Owner accepts identity assurance/recovery policy and selected database/session/email choices; root records ADR. |
| [T9.1](../tasks/T9.1.md) | L12 | S1 | None |
| [T10.1](../tasks/T10.1.md) | L13 | S1 | None |
| [T15.1](../tasks/T15.1.md) | L16 | S1 | None |
| [T8.1](../tasks/T8.1.md) | L10 | S2 | None |
| [T11.1](../tasks/T11.1.md) | L14 | S3 | Revalidate official standards and SDK/client versions before dispatch; unsupported required clients need a separately reviewed profile amendment. |

## Wave 4: 8 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.7](../tasks/T1.7.md) | L02 | S0 | Original-work license selection. |
| [T1.8](../tasks/T1.8.md) | L01 | S0 | None |
| [T3.2](../tasks/T3.2.md) | L03 | S1 | None |
| [T4.1](../tasks/T4.1.md) | L05 | S1 | Owner accepts role, organization lifecycle and enforcement policy; root records ADR. |
| [T9.2](../tasks/T9.2.md) | L12 | S1 | None |
| [T8.2](../tasks/T8.2.md) | L10 | S2 | None |
| [T10.7](../tasks/T10.7.md) | L13 | S2 | None |
| [T11.8](../tasks/T11.8.md) | L14 | S3 | A failing isolation or live-policy test blocks OAuth release until a narrow adapter or reviewed upstream fix passes. |

## Wave 5: 10 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.9](../tasks/T1.9.md) | L01 | S0 | None |
| [T2.1](../tasks/T2.1.md) | L02 | S1 | None |
| [T3.4](../tasks/T3.4.md) | L03 | S1 | None |
| [T4.2](../tasks/T4.2.md) | L05 | S1 | None |
| [T5.1](../tasks/T5.1.md) | L06 | S1 | Owner approves catalog, seat/usage rules, tax responsibility, grace/refund/dispute policy and test/live account boundary; root records ADR. |
| [T8.3](../tasks/T8.3.md) | L10 | S2 | Creating state storage and KMS resources incurs cost and requires owner-authorized bootstrap credentials. |
| [T8.8](../tasks/T8.8.md) | L11 | S2 | None |
| [T9.4](../tasks/T9.4.md) | L12 | S2 | None |
| [T10.10](../tasks/T10.10.md) | L13 | S2 | None |
| [T15.6](../tasks/T15.6.md) | L16 | S2 | Revalidate official provenance/signing/CI specifications and chosen versions before implementation. |

## Wave 6: 10 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.10](../tasks/T1.10.md) | L01 | S1 | None |
| [T2.3](../tasks/T2.3.md) | L02 | S1 | None |
| [T4.3](../tasks/T4.3.md) | L05 | S1 | None |
| [T5.2](../tasks/T5.2.md) | L06 | S1 | None |
| [T6.1](../tasks/T6.1.md) | L08 | S1 | None |
| [T7.1](../tasks/T7.1.md) | L09 | S1 | None |
| [T9.8](../tasks/T9.8.md) | L12 | S1 | None |
| [T8.11](../tasks/T8.11.md) | L10 | S2 | Creating upstream issues or PRs and modifying public upstream contracts requires explicit task authorization and maintainer review. |
| [T11.2](../tasks/T11.2.md) | L14 | S3 | None |
| [T10.8](../tasks/T10.8.md) | L13 | S4 | Backup bucket creation, storage charges and retention/destruction policy require authorized owner configuration. |

## Wave 7: 7 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T1.11](../tasks/T1.11.md) | L02 | S1 | None |
| [T1.12](../tasks/T1.12.md) | L01 | S1 | None |
| [T5.3](../tasks/T5.3.md) | L06 | S1 | None |
| [T7.2](../tasks/T7.2.md) | L09 | S1 | None |
| [T8.19](../tasks/T8.19.md) | L10 | S2 | None |
| [T9.5](../tasks/T9.5.md) | L12 | S2 | Registry publication/signing requires explicitly authorized trusted CI environment, repository permissions and owner cloud role. |
| [T11.3](../tasks/T11.3.md) | L14 | S3 | None |

## Wave 8: 9 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T2.2](../tasks/T2.2.md) | L01 | S1 | None |
| [T2.4](../tasks/T2.4.md) | L02 | S1 | None |
| [T3.6](../tasks/T3.6.md) | L03 | S1 | None |
| [T5.4](../tasks/T5.4.md) | L06 | S1 | None |
| [T7.3](../tasks/T7.3.md) | L09 | S1 | None |
| [T8.20](../tasks/T8.20.md) | L10 | S2 | None |
| [T8.21](../tasks/T8.21.md) | L11 | S2 | ALB, ACM validation and DNS writes require exact account/region/domain authorization; DNS and certificate issuance time are external dependencies. |
| [T9.6](../tasks/T9.6.md) | L12 | S3 | Creating GitHub releases or publishing artifacts is an external action requiring maintainer authorization. |
| [T15.9](../tasks/T15.9.md) | L16 | S4 | None |

## Wave 9: 11 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T3.3](../tasks/T3.3.md) | L03 | S1 | None |
| [T5.5](../tasks/T5.5.md) | L06 | S1 | None |
| [T6.2](../tasks/T6.2.md) | L08 | S1 | None |
| [T7.4](../tasks/T7.4.md) | L09 | S1 | None |
| [T10.2](../tasks/T10.2.md) | L13 | S1 | None |
| [T12.1](../tasks/T12.1.md) | L14 | S1 | None |
| [T16.1](../tasks/T16.1.md) | L01 | S1 | None |
| [T8.22](../tasks/T8.22.md) | L10 | S2 | None |
| [T8.27](../tasks/T8.27.md) | L11 | S2 | Creating SES identities/DNS records and granting send permissions requires explicit owner authorization for the sender domain and selected region. |
| [T1.13](../tasks/T1.13.md) | L02 | S3 | None |
| [T9.7](../tasks/T9.7.md) | L12 | S3 | None |

## Wave 10: 11 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T2.6](../tasks/T2.6.md) | L08 | S1 | None |
| [T3.5](../tasks/T3.5.md) | L03 | S1 | None |
| [T4.4](../tasks/T4.4.md) | L05 | S1 | None |
| [T5.7](../tasks/T5.7.md) | L06 | S1 | None |
| [T7.5](../tasks/T7.5.md) | L09 | S1 | None |
| [T10.3](../tasks/T10.3.md) | L13 | S1 | None |
| [T12.2](../tasks/T12.2.md) | L14 | S1 | None |
| [T8.14](../tasks/T8.14.md) | L10 | S2 | None |
| [T8.28](../tasks/T8.28.md) | L11 | S2 | Sending any real email requires explicit authorization for its recipient and content. Production-access requests and provider-account changes require owner approval; default verification remains non-sending. |
| [T3.10](../tasks/T3.10.md) | L04 | S3 | None |
| [T9.15](../tasks/T9.15.md) | L12 | S3 | None |

## Wave 11: 9 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T3.7](../tasks/T3.7.md) | L03 | S1 | None |
| [T5.8](../tasks/T5.8.md) | L06 | S1 | None |
| [T6.4](../tasks/T6.4.md) | L08 | S1 | None |
| [T7.7](../tasks/T7.7.md) | L09 | S1 | None |
| [T8.15](../tasks/T8.15.md) | L11 | S2 | None |
| [T12.4](../tasks/T12.4.md) | L14 | S2 | None |
| [T3.22](../tasks/T3.22.md) | L04 | S3 | Separate AMSL worktree/assignment, maintainer security review and published immutable release; root owns AMOS dependency pin. |
| [T8.4](../tasks/T8.4.md) | L10 | S3 | None |
| [T15.4](../tasks/T15.4.md) | L16 | S3 | None |

## Wave 12: 12 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T2.8](../tasks/T2.8.md) | L01 | S1 | None |
| [T3.8](../tasks/T3.8.md) | L03 | S1 | None |
| [T5.6](../tasks/T5.6.md) | L06 | S1 | None |
| [T8.17](../tasks/T8.17.md) | L11 | S2 | None |
| [T8.23](../tasks/T8.23.md) | L09 | S2 | None |
| [T15.3](../tasks/T15.3.md) | L16 | S2 | None |
| [T3.11](../tasks/T3.11.md) | L04 | S3 | None |
| [T5.13](../tasks/T5.13.md) | L07 | S3 | None |
| [T6.14](../tasks/T6.14.md) | L08 | S3 | None |
| [T8.5](../tasks/T8.5.md) | L10 | S3 | None |
| [T10.4](../tasks/T10.4.md) | L13 | S3 | None |
| [T11.9](../tasks/T11.9.md) | L14 | S3 | None |

## Wave 13: 12 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T5.9](../tasks/T5.9.md) | L06 | S1 | None |
| [T6.3](../tasks/T6.3.md) | L08 | S1 | None |
| [T12.3](../tasks/T12.3.md) | L14 | S1 | None |
| [T7.9](../tasks/T7.9.md) | L11 | S2 | None |
| [T8.18](../tasks/T8.18.md) | L10 | S2 | None |
| [T2.9](../tasks/T2.9.md) | L01 | S3 | None |
| [T3.9](../tasks/T3.9.md) | L03 | S3 | None |
| [T3.12](../tasks/T3.12.md) | L04 | S3 | None |
| [T5.14](../tasks/T5.14.md) | L07 | S3 | None |
| [T9.9](../tasks/T9.9.md) | L12 | S3 | None |
| [T14.10](../tasks/T14.10.md) | L16 | S3 | AMSL maintainers control upstream merge/release and maturity promotion; AMOS owner automation cannot override that authority. |
| [T10.5](../tasks/T10.5.md) | L13 | S4 | None |

## Wave 14: 11 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T2.5](../tasks/T2.5.md) | L02 | S1 | None |
| [T5.22](../tasks/T5.22.md) | L06 | S1 | None |
| [T6.5](../tasks/T6.5.md) | L08 | S1 | None |
| [T7.6](../tasks/T7.6.md) | L09 | S1 | None |
| [T7.10](../tasks/T7.10.md) | L11 | S2 | None |
| [T12.8](../tasks/T12.8.md) | L14 | S2 | E16 executes authorized cloud probes; static policy validation alone cannot certify live origin isolation. |
| [T3.13](../tasks/T3.13.md) | L04 | S3 | None |
| [T8.6](../tasks/T8.6.md) | L10 | S3 | None |
| [T9.10](../tasks/T9.10.md) | L12 | S4 | None |
| [T10.6](../tasks/T10.6.md) | L13 | S4 | External notification channels and telemetry destinations require owner configuration and authorization; no messages are sent during planning. |
| [T14.1](../tasks/T14.1.md) | L16 | S4 | None |

## Wave 15: 9 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T6.18](../tasks/T6.18.md) | L08 | S1 | None |
| [T7.13](../tasks/T7.13.md) | L09 | S1 | None |
| [T7.11](../tasks/T7.11.md) | L11 | S2 | Real cloud apply requires owner authorization for the exact account, region, domain, resources, cost and plan; E16 performs combined live qualification. |
| [T9.14](../tasks/T9.14.md) | L12 | S2 | Live GitHub OIDC trust and environment protections require owner repository/cloud administration; mocks alone do not establish trust. |
| [T3.14](../tasks/T3.14.md) | L04 | S3 | None |
| [T8.7](../tasks/T8.7.md) | L10 | S3 | None |
| [T11.4](../tasks/T11.4.md) | L14 | S3 | None |
| [T10.9](../tasks/T10.9.md) | L13 | S4 | None |
| [T14.2](../tasks/T14.2.md) | L16 | S4 | None |

## Wave 16: 8 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T7.14](../tasks/T7.14.md) | L09 | S1 | None |
| [T9.3](../tasks/T9.3.md) | L12 | S1 | None |
| [T5.10](../tasks/T5.10.md) | L06 | S2 | Owner supplies approved Stripe account, test-mode products/secrets, endpoint/domain and sandbox budget; live charges require E16 explicit authorization. |
| [T3.15](../tasks/T3.15.md) | L04 | S3 | None |
| [T6.7](../tasks/T6.7.md) | L08 | S3 | None |
| [T8.9](../tasks/T8.9.md) | L11 | S3 | DNS record creation or replacement requires authorization for the exact zone and hostname; delegation must already be active. |
| [T12.5](../tasks/T12.5.md) | L14 | S3 | None |
| [T14.3](../tasks/T14.3.md) | L16 | S4 | None |

## Wave 17: 6 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T2.7](../tasks/T2.7.md) | L02 | S1 | None |
| [T3.16](../tasks/T3.16.md) | L04 | S3 | None |
| [T8.10](../tasks/T8.10.md) | L11 | S3 | Public certificate issuance requires domain control and a reachable host; ACME rate limits and DNS propagation are external dependencies. |
| [T12.6](../tasks/T12.6.md) | L14 | S3 | None |
| [T9.11](../tasks/T9.11.md) | L12 | S4 | None |
| [T14.7](../tasks/T14.7.md) | L16 | S4 | Security maintainer supplies and verifies a private disclosure channel before external intake is enabled. |

## Wave 18: 7 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.2](../tasks/T16.2.md) | L01 | S1 | None |
| [T3.17](../tasks/T3.17.md) | L03 | S3 | None |
| [T8.12](../tasks/T8.12.md) | L10 | S3 | None |
| [T8.16](../tasks/T8.16.md) | L11 | S3 | Optional Cloudflare proxy setup and any paid plan feature require explicit owner choice, scoped credentials and current provider capability verification. |
| [T12.7](../tasks/T12.7.md) | L14 | S3 | None |
| [T9.12](../tasks/T9.12.md) | L12 | S4 | None |
| [T15.5](../tasks/T15.5.md) | L16 | S4 | None |

## Wave 19: 11 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.3](../tasks/T16.3.md) | L01 | S2 | Owner-approved provider accounts, credentials, test catalog and callback setup |
| [T3.18](../tasks/T3.18.md) | L03 | S3 | None |
| [T4.5](../tasks/T4.5.md) | L05 | S3 | None |
| [T6.9](../tasks/T6.9.md) | L08 | S3 | None |
| [T7.8](../tasks/T7.8.md) | L09 | S3 | None |
| [T8.13](../tasks/T8.13.md) | L10 | S3 | None |
| [T11.12](../tasks/T11.12.md) | L14 | S3 | None |
| [T7.12](../tasks/T7.12.md) | L11 | S4 | None |
| [T9.13](../tasks/T9.13.md) | L12 | S4 | GitHub App installation, branch pushes and pull-request creation require explicit repository authorization. |
| [T10.12](../tasks/T10.12.md) | L13 | S4 | Live rollback mutates the deployed service and requires operator authorization or a previously authorized bounded recovery policy. |
| [T15.7](../tasks/T15.7.md) | L16 | S4 | None |

## Wave 20: 11 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.4](../tasks/T16.4.md) | L01 | S2 | Explicit infrastructure budget, deployment approval and domain ownership |
| [T3.19](../tasks/T3.19.md) | L03 | S3 | None |
| [T4.6](../tasks/T4.6.md) | L05 | S3 | None |
| [T4.9](../tasks/T4.9.md) | L04 | S3 | None |
| [T6.8](../tasks/T6.8.md) | L08 | S3 | None |
| [T7.15](../tasks/T7.15.md) | L09 | S3 | None |
| [T12.9](../tasks/T12.9.md) | L14 | S3 | None |
| [T8.26](../tasks/T8.26.md) | L11 | S4 | Independent live qualification of both profiles requires separately approved resources and cost; E16 owns combined execution. |
| [T9.16](../tasks/T9.16.md) | L12 | S4 | None |
| [T10.14](../tasks/T10.14.md) | L13 | S4 | None |
| [T14.11](../tasks/T14.11.md) | L16 | S4 | External maintainer release/review availability; dependency edit belongs to root integrator. |

## Wave 21: 7 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.5](../tasks/T16.5.md) | L01 | S2 | Approved disposable recovery resources |
| [T4.7](../tasks/T4.7.md) | L05 | S3 | None |
| [T6.6](../tasks/T6.6.md) | L08 | S3 | None |
| [T7.16](../tasks/T7.16.md) | L09 | S4 | None |
| [T9.17](../tasks/T9.17.md) | L12 | S4 | None |
| [T12.12](../tasks/T12.12.md) | L14 | S4 | None |
| [T15.8](../tasks/T15.8.md) | L16 | S4 | None |

## Wave 22: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T4.8](../tasks/T4.8.md) | L05 | S3 | None |
| [T15.10](../tasks/T15.10.md) | L16 | S4 | None |

## Wave 23: 6 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T4.10](../tasks/T4.10.md) | L04 | S3 | None |
| [T4.12](../tasks/T4.12.md) | L05 | S3 | None |
| [T5.11](../tasks/T5.11.md) | L06 | S3 | None |
| [T5.15](../tasks/T5.15.md) | L07 | S3 | None |
| [T11.5](../tasks/T11.5.md) | L14 | S3 | None |
| [T14.13](../tasks/T14.13.md) | L16 | S5 | None |

## Wave 24: 7 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T3.21](../tasks/T3.21.md) | L04 | S3 | Owner provider registrations, email sender/domain approval, device access and explicit sandbox usage authorization; production only under E16. |
| [T4.11](../tasks/T4.11.md) | L05 | S3 | None |
| [T5.12](../tasks/T5.12.md) | L06 | S3 | None |
| [T5.16](../tasks/T5.16.md) | L07 | S3 | None |
| [T6.10](../tasks/T6.10.md) | L08 | S3 | None |
| [T11.6](../tasks/T11.6.md) | L14 | S3 | None |
| [T14.4](../tasks/T14.4.md) | L16 | S5 | None |

## Wave 25: 5 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T5.17](../tasks/T5.17.md) | L06 | S3 | None |
| [T5.18](../tasks/T5.18.md) | L07 | S3 | None |
| [T6.11](../tasks/T6.11.md) | L08 | S3 | None |
| [T11.7](../tasks/T11.7.md) | L14 | S3 | None |
| [T14.5](../tasks/T14.5.md) | L16 | S5 | None |

## Wave 26: 5 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T3.20](../tasks/T3.20.md) | L03 | S3 | None |
| [T4.13](../tasks/T4.13.md) | L05 | S3 | None |
| [T5.19](../tasks/T5.19.md) | L07 | S3 | None |
| [T11.10](../tasks/T11.10.md) | L14 | S3 | None |
| [T14.6](../tasks/T14.6.md) | L16 | S5 | None |

## Wave 27: 7 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T4.14](../tasks/T4.14.md) | L05 | S3 | None |
| [T5.20](../tasks/T5.20.md) | L07 | S3 | Approved real Stripe test products/meters, account access and sandbox budget; E16 governs any live billing. |
| [T6.12](../tasks/T6.12.md) | L08 | S3 | None |
| [T11.11](../tasks/T11.11.md) | L14 | S3 | None |
| [T3.23](../tasks/T3.23.md) | L03 | S4 | None |
| [T5.21](../tasks/T5.21.md) | L06 | S4 | None |
| [T14.14](../tasks/T14.14.md) | L16 | S5 | Upstream maintainer account, infrastructure budget and deployment authorization |

## Wave 28: 4 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T6.13](../tasks/T6.13.md) | L08 | S3 | None |
| [T11.13](../tasks/T11.13.md) | L14 | S3 | None |
| [T16.7](../tasks/T16.7.md) | L01 | S3 | Approved payment test environment |
| [T10.11](../tasks/T10.11.md) | L13 | S4 | Production restore, credential rotation and traffic cutover require explicit operator authorization for the exact target and recovery point. |

## Wave 29: 4 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T6.16](../tasks/T6.16.md) | L08 | S3 | None |
| [T11.14](../tasks/T11.14.md) | L14 | S3 | External client accounts/availability and E16 live qualification; no credentials or provider success may be simulated. |
| [T8.24](../tasks/T8.24.md) | L13 | S4 | None |
| [T15.13](../tasks/T15.13.md) | L16 | S4 | Provider-live isolated restore drill and measured recovery evidence are E16 gates, not established by fixtures alone. |

## Wave 30: 3 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T12.10](../tasks/T12.10.md) | L14 | S3 | None |
| [T16.6](../tasks/T16.6.md) | L01 | S3 | Approved external provider configurations/devices |
| [T8.25](../tasks/T8.25.md) | L13 | S4 | Profile migration creates resources, may incur dual-running cost and changes production data/traffic; exact owner approval and recovery point are required. |

## Wave 31: 4 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T6.15](../tasks/T6.15.md) | L08 | S3 | None |
| [T12.11](../tasks/T12.11.md) | L14 | S3 | None |
| [T15.2](../tasks/T15.2.md) | L16 | S3 | None |
| [T10.13](../tasks/T10.13.md) | L13 | S4 | Actual credential rotation/revocation requires explicit authority over each affected provider and runtime environment. |

## Wave 32: 4 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.8](../tasks/T16.8.md) | L01 | S3 | None |
| [T6.17](../tasks/T6.17.md) | L08 | S4 | None |
| [T10.15](../tasks/T10.15.md) | L13 | S4 | None |
| [T15.12](../tasks/T15.12.md) | L16 | S4 | None |

## Wave 33: 3 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T10.16](../tasks/T10.16.md) | L13 | S4 | None |
| [T16.9](../tasks/T16.9.md) | L01 | S4 | Approved test repository for real upgrade PR verification |
| [T13.1](../tasks/T13.1.md) | L15 | S5 | None |

## Wave 34: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T10.17](../tasks/T10.17.md) | L13 | S4 | None |
| [T13.2](../tasks/T13.2.md) | L15 | S5 | Owner chooses runtime/provider and configures credentials/spend policy before live runs; no task authorizes paid execution. |

## Wave 35: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.3](../tasks/T13.3.md) | L15 | S5 | None |

## Wave 36: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.4](../tasks/T13.4.md) | L15 | S5 | None |

## Wave 37: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.5](../tasks/T13.5.md) | L15 | S5 | Chosen runner must provide demonstrated isolation; process labels or prompt instructions alone do not satisfy this gate. |

## Wave 38: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.6](../tasks/T13.6.md) | L15 | S5 | None |
| [T14.8](../tasks/T14.8.md) | L16 | S5 | None |

## Wave 39: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.7](../tasks/T13.7.md) | L15 | S5 | None |

## Wave 40: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.8](../tasks/T13.8.md) | L15 | S5 | None |

## Wave 41: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.9](../tasks/T13.9.md) | L15 | S5 | None |
| [T15.11](../tasks/T15.11.md) | L16 | S5 | The actual selected runtime sandbox must be exercised; mocked network denial alone is insufficient. |

## Wave 42: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.10](../tasks/T13.10.md) | L15 | S5 | None |

## Wave 43: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.11](../tasks/T13.11.md) | L15 | S5 | None |
| [T14.9](../tasks/T14.9.md) | L16 | S5 | Actual upstream publication and protected identity setup require repository-owner authorization; E16 records live evidence. |

## Wave 44: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.12](../tasks/T13.12.md) | L15 | S5 | None |
| [T14.12](../tasks/T14.12.md) | L16 | S5 | Live reporting/publication/upgrade checks only after authorized owner and upstream configuration in E16. |

## Wave 45: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T13.13](../tasks/T13.13.md) | L15 | S5 | Owner-authorized live maintenance/provider/deploy drill in E16 remains required before autonomous operation is advertised. |
| [T15.14](../tasks/T15.14.md) | L16 | S5 | None |

## Wave 46: 2 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T15.15](../tasks/T15.15.md) | L16 | S5 | Independent security reviewer and E16 live deployment/recovery evidence required; security-sensitive AMSL review remains a separate maintainer gate. |
| [T16.10](../tasks/T16.10.md) | L01 | S5 | Approved isolated agent execution/model budget |

## Wave 47: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.11](../tasks/T16.11.md) | L01 | S5 | Approved test repositories and release identities |

## Wave 48: 1 task slots

| Task | Lane | Stage | External gate |
|---|---|---|---|
| [T16.12](../tasks/T16.12.md) | L01 | S5 | Maintainer release/publication authorization |
