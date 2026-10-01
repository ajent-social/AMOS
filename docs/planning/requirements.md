# Confirmed requirement coverage

Status: requirements are planned, not implemented. Links identify concrete task contracts and their acceptance boundaries. Stage labels describe sequencing, not capability availability today.

| ID | Requirement | Planned implementation / qualification |
|---|---|---|
| R01 | Complete new paid SaaS foundation | E1-E6; T2.4-T2.7, T16.2-T16.4 |
| R02 | Owner-hosted installations and accounts | T7.1, T8.2, T8.23, T16.4 |
| R03 | `amos init` prepares app, workflows and IaC | T7.1-T7.14, T9.1-T9.3, T9.8 |
| R04 | `amos deploy` makes the reviewed app live | T7.9-T7.11, T8.15/T8.22, T9.14, T16.4 |
| R05 | One public domain; shared versus business routes | T2.2, T12.1, T12.3-T12.9, T16.8 |
| R06 | Integrated default and arbitrary-stack private service | T2.3-T2.6, T7.8, T12.4-T12.11 |
| R07 | Go SSR, HTMX, plain JavaScript/CSS default | T6.1-T6.5, T2.6, T6.16 |
| R08 | AWS first with Cloudflare required | E8; T8.8/T8.21, T16.4 |
| R09 | Both managed and small-VM profiles selected per install | T8.1, T8.4-T8.7, T8.19-T8.26 |
| R10 | Complete web app and APIs/MCP first; other user clients later | E6, E11-E12, T16.8; explicit non-goals in master plan |
| R11 | Personal accounts and organization membership | T3.2-T3.5, T4.1-T4.14, T6.4/T6.10 |
| R12 | Separate personal and organization subscriptions | T5.1-T5.4, T5.17, T16.7 |
| R13 | Flat monthly/yearly, seat and usage pricing | T5.4, T5.13-T5.20, T6.12, T16.7 |
| R14 | Stripe first, future-provider adapter | T5.2/T5.5/T5.7/T5.18; provider gaps explicit |
| R15 | Email/password and email magic-link sign-in | T3.4-T3.9, T6.3/T6.7, T3.21 |
| R16 | Google, GitHub and Apple sign-in | T3.10-T3.13/T3.22, T6.7, T3.21 |
| R17 | Passkeys | T3.14/T3.18, T6.8, T3.21 |
| R18 | Per-organization OIDC enterprise SSO | T4.9-T4.10, T6.11, T16.6 |
| R19 | MFA with organization enforcement | T3.15-T3.17, T4.11, T6.9/T6.11 |
| R20 | Profiles, invitations, roles and account administration | E3-E4, T6.6/T6.10/T6.13, T16.6 |
| R21 | Customer-created API keys and MCP OAuth | T11.5-T11.12, T11.14 |
| R22 | Full applicable agent access under application controls | T2.8, T11.13, T12.10, T15.2, T16.8 |
| R23 | Independent agents/external runtimes; no mandatory governance/runtime | ADR 001, T11.1/T11.14, T13.2; no customer approval-inbox epic |
| R24 | OpenAPI first; generated Go scaffolding and MCP | T2.1/T2.3, T11.2-T11.4, T12.2/T12.12 |
| R25 | All shared UI replaceable | T6.14-T6.17, T12.10-T12.11, T16.9 |
| R26 | Hybrid versioned core, customizable UI, managed deployment files | T9.8-T9.12, T6.17, T12.12 |
| R27 | Automated upgrade PRs | T9.13/T9.16, T14.11, T16.9 |
| R28 | Deployment, monitoring, backup and recovery tooling | E7-E10; T16.4-T16.5 |
| R29 | Autonomous diagnosis, fixes, releases and eligible deployment | E13, T15.7-T15.14, T16.10 |
| R30 | Shared fixes upstream; business and custom UI fixes private | T13.7, E14, T16.10-T16.11 |
| R31 | Default sanitized upstream diagnostics, no raw/private data | T14.1-T14.7, T15.4/T15.11, T16.11 |
| R32 | Owner-controlled application agents; separate upstream maintenance | T13.1-T13.6, T14.8/T14.13-T14.14 |
| R33 | Full-scope plan, staged delivery and inexpensive parallel workers | ADR 007, E1-E16, per-task contracts and execution/wave guide |
| R34 | Public open-source information boundary | T1.4/T1.6/T1.7, T14.1/T14.7, publication checks |

## Recommended AMSL direction, kept distinct from confirmed requirements

ADR 006 proposes qualified AMSL consumption and evidence-driven upstream improvement. T1.4 inventories public candidates, T3.22/T11.8 qualify concrete identity seams, T8.11 addresses infrastructure/workflow gaps, and T14.10-T14.11 package publishable evidence and consume reviewed releases. This does not authorize private source copying or bypass upstream review.

## Not silently inherited from earlier brainstorming

No required Kubernetes cluster, autonomous cloud arbitrage, native app clients in the first product scope, SAML, directory sync, proprietary governance dependency, speculative future agent API or full vendor-product parity is assumed. The selected staged requirements above remain the source of truth.
