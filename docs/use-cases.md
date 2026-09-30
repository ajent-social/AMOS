# Use cases

All use cases are PLANNED. Interface paths are proposed contracts, not implemented endpoints. The canonical machine-readable manifest is `docs/usecases-manifest.json`.

| ID | Domain | Outcome | Tasks |
|---|---|---|---|
| UC-001 | foundation | Initialize a developer-owned application | T2.7 |
| UC-002 | foundation | Compose a working local application | T1.1, T1.5, T1.10, T1.11, T2.2 |
| UC-003 | contracts | Declare a business operation once | T1.2, T2.1, T2.3 |
| UC-004 | contracts | Verify route and policy registration | T1.2, T1.5, T1.12, T2.2, T2.8, T2.9 |
| UC-005 | foundation | Understand supported dependencies | T1.2, T1.4, T1.6 |
| UC-006 | storage | Apply compatible schema migrations | T1.3 |
| UC-007 | reference | Exercise a reference paid feature | T2.4, T2.5, T2.6 |
| UC-008 | documentation | Resume development from public documentation | T1.1, T1.9, T1.13, T2.7 |
| UC-009 | contribution | Submit a compatible contribution | T1.4, T1.6, T1.7, T1.8, T1.9 |
| UC-010 | contracts | Regenerate without losing custom work | T2.1, T2.3 |
| UC-020 | identity | Register and verify an account | T3.1, T3.2, T3.4, T3.5, T3.6, T3.7, T4.3, T6.3 |
| UC-021 | identity | Sign in with email and password | T3.1, T3.3, T3.4, T3.5, T3.7, T6.3 |
| UC-022 | identity | Recover or change password | T3.1, T3.6, T3.7, T3.8, T6.3 |
| UC-023 | identity | Sign in by email magic link | T3.9, T3.21, T6.7 |
| UC-024 | identity | Sign in with Google | T3.10, T3.11, T3.21, T3.22, T6.7 |
| UC-025 | identity | Sign in with GitHub | T3.10, T3.12, T3.21, T3.22, T6.7 |
| UC-026 | identity | Sign in with Apple | T3.10, T3.13, T3.21, T3.22, T6.7 |
| UC-027 | identity | Enroll and use passkeys | T3.1, T3.7, T3.14, T3.21, T6.8 |
| UC-028 | identity | Enroll, satisfy and recover MFA | T3.1, T3.15, T3.16, T3.17, T3.21, T4.10, T4.11, T6.9, T6.11 |
| UC-029 | identity | Manage profile, email and sign-in methods | T3.1, T3.2, T3.6, T3.8, T3.10, T3.16, T3.17, T3.18, T3.19, T6.6, T6.8, T6.9 |
| UC-030 | identity | Inspect and revoke sessions | T3.1, T3.3, T3.8, T3.19, T3.23, T6.6 |
| UC-031 | identity | Export or delete personal account | T3.2, T3.20, T5.17, T6.13 |
| UC-032 | identity | Disable account and invalidate authority | T3.1, T3.2, T3.3, T3.17, T3.20, T3.23, T6.13 |
| UC-033 | workspace | Use personal workspace and switch organizations | T4.1, T4.2, T4.3, T4.4, T6.4 |
| UC-034 | workspace | Create an organization | T4.1, T4.2, T4.5, T6.10 |
| UC-035 | workspace | Invite and join organization | T4.1, T4.2, T4.6, T4.7, T5.14, T6.10 |
| UC-036 | workspace | Manage roles, leave or remove members | T4.1, T4.2, T4.7, T4.8, T4.12, T4.14, T6.10 |
| UC-037 | workspace | Transfer, suspend, export or delete organization | T4.1, T4.5, T4.11, T4.12, T4.13, T4.14, T5.17, T6.10, T6.13 |
| UC-038 | workspace | Access only authorized tenant resources | T4.1, T4.4, T4.8, T4.13, T4.14, T6.4 |
| UC-039 | workspace | Configure and enforce organization OIDC SSO | T4.1, T4.9, T4.10, T4.11, T4.14, T6.11 |
| UC-040 | billing | Buy independent personal subscription | T5.1, T5.2, T5.3, T5.4, T5.5, T5.6, T5.7, T5.8, T5.9, T5.10, T5.21, T6.5 |
| UC-041 | billing | Buy independent organization subscription | T5.1, T5.2, T5.3, T5.4, T5.5, T5.6, T5.7, T5.8, T5.9, T5.10, T6.5 |
| UC-042 | billing | Manage subscription, invoices and payment actions | T5.1, T5.2, T5.3, T5.11, T5.12, T5.17, T5.22, T6.12, T6.18 |
| UC-043 | billing | Pay and reconcile organization seats | T5.1, T5.2, T5.3, T5.13, T5.14, T5.20, T5.21, T6.12, T6.18 |
| UC-044 | billing | Record and inspect usage-based billing | T5.1, T5.2, T5.15, T5.16, T5.17, T5.18, T5.19, T5.20, T5.21, T6.12, T6.18 |
| UC-045 | billing | Understand denied or unavailable paid access | T5.1, T5.2, T5.4, T5.7, T5.8, T5.9, T5.10, T5.12, T5.19, T5.21, T6.5, T6.12, T6.18 |
| UC-046 | ui | Complete lifecycle through default shared pages | T6.1, T6.2, T6.3, T6.4, T6.5, T6.6, T6.7, T6.8, T6.9, T6.10, T6.11, T6.12, T6.13, T6.15, T6.16, T6.18 |
| UC-047 | ui | Theme or replace an individual shared page | T6.1, T6.2, T6.14, T6.17 |
| UC-048 | ui | Replace the whole shared user interface | T6.1, T6.15, T6.17 |
| UC-049 | ui | Use lifecycle with keyboard and small screen | T6.1, T6.2, T6.14, T6.16 |
| UC-050 | initializer | Initialize a named application | T7.1, T7.2, T7.13, T7.14, T7.16, T8.23, T9.3, T9.8 |
| UC-051 | initializer | Diagnose local prerequisites | T7.3, T7.13, T7.15, T8.28 |
| UC-052 | initializer | Run the local reference application | T7.4, T7.5, T7.6, T7.13, T7.14, T9.3, T10.2 |
| UC-053 | initializer | Resume an interrupted local setup | T7.1, T7.2, T7.7, T7.12, T7.13 |
| UC-054 | initializer | Choose an integrated or separate business service | T7.8, T7.15, T8.10, T8.21 |
| UC-055 | initializer | Inspect deployment inputs before mutation | T7.9, T7.10, T7.14, T7.15, T8.15, T8.22, T8.23, T8.26, T9.14 |
| UC-056 | initializer | Deploy an approved immutable application release | T7.11, T7.12, T7.15, T7.16, T8.13, T8.22, T8.25, T8.26, T9.5 |
| UC-057 | initializer | Inspect, stop, and clean local resources | T7.5, T7.7, T7.13 |
| UC-058 | infrastructure | Select a costed AWS hosting profile | T8.1, T8.14, T8.19, T8.23, T10.16 |
| UC-059 | infrastructure | Bootstrap owner-controlled infrastructure state | T8.2, T8.3, T8.11, T8.12, T8.17, T8.22, T9.14 |
| UC-060 | infrastructure | Provision an isolated AWS application host | T8.1, T8.4, T8.5, T8.6, T8.11, T8.12, T8.17, T8.18, T8.19, T8.20, T8.22, T8.26, T8.27, T8.28 |
| UC-061 | infrastructure | Connect a required Cloudflare domain | T8.8, T8.9, T8.10, T8.11, T8.12, T8.16, T8.17, T8.18, T8.21, T8.26, T8.27 |
| UC-062 | infrastructure | Keep cloud credentials out of application artifacts | T7.9, T8.2, T8.4, T8.6, T8.19, T8.27, T8.28, T9.14, T10.13 |
| UC-063 | infrastructure | Persist and recover PostgreSQL in either profile | T7.4, T8.1, T8.5, T8.7, T8.12, T8.17, T8.20, T8.24, T8.25, T8.26, T10.7, T10.10, T10.16 |
| UC-064 | infrastructure | Protect cost and retained resources during changes | T7.10, T8.14, T8.15, T8.17, T8.18, T8.23, T8.24 |
| UC-065 | delivery | Validate public contributions without cloud privilege | T9.1, T9.2, T9.3, T9.14, T9.17 |
| UC-066 | delivery | Build and verify immutable release artifacts | T8.11, T8.13, T8.22, T9.1, T9.4, T9.5, T9.6, T9.15, T9.17 |
| UC-067 | delivery | Install a versioned CLI safely | T9.6, T9.7 |
| UC-068 | delivery | Upgrade generated files without losing custom code | T9.8, T9.10, T9.11, T9.12, T9.15, T9.16, T9.17 |
| UC-069 | delivery | Receive and review an upgrade pull request | T9.13, T9.16 |
| UC-070 | delivery | Preserve overrides and package extension contracts | T7.14, T9.8, T9.9, T9.10, T9.16 |
| UC-071 | delivery | Reject incompatible or untrusted upgrades | T9.9, T9.10, T9.12, T9.15, T9.16 |
| UC-072 | operations | Observe application health without exposing internals | T8.10, T8.21, T10.1, T10.2, T10.17 |
| UC-073 | operations | Trace requests and business-side failures | T8.16, T10.1, T10.3, T10.4, T10.5, T10.14, T10.17 |
| UC-074 | operations | Detect failed background processing | T8.28, T10.1, T10.4, T10.6, T10.9, T10.15 |
| UC-075 | operations | Create encrypted application backups | T8.7, T8.20, T10.1, T10.7, T10.8, T10.9, T10.15, T10.17 |
| UC-076 | operations | Restore into an isolated target | T8.17, T8.20, T8.24, T8.25, T8.26, T10.1, T10.10, T10.11, T10.15, T10.17 |
| UC-077 | operations | Recover an unsuccessful release | T7.12, T8.13, T8.24, T9.12, T10.12, T10.15 |
| UC-078 | operations | Operate within declared availability and cost limits | T8.1, T8.24, T8.25, T10.1, T10.6, T10.8, T10.9, T10.14, T10.15, T10.16 |
| UC-079 | operations | Rotate secrets and export diagnostic evidence | T10.3, T10.13, T10.14, T10.15, T10.17 |
| UC-080 | agent-access | Discover only eligible MCP tools | T11.1, T11.2, T11.3, T11.4, T11.13 |
| UC-081 | agent-access | Invoke domain behavior with REST/MCP parity | T11.2, T11.4, T11.6, T11.11, T11.12, T11.13, T12.3, T12.9, T12.10, T15.2 |
| UC-082 | agent-access | Create and manage bounded API keys | T11.5, T11.7, T12.11, T15.4 |
| UC-083 | agent-access | Reject stale or overpowered keys | T11.5, T11.6, T11.13, T15.2 |
| UC-084 | agent-access | Authorize public MCP clients | T11.1, T11.8, T11.9, T11.10, T11.14, T12.11, T15.3 |
| UC-085 | agent-access | Refresh or revoke MCP access | T11.8, T11.10, T11.11, T11.14, T15.2, T15.4 |
| UC-086 | agent-access | Resume required human proof | T11.10, T11.12, T11.13, T12.10, T12.11 |
| UC-087 | agent-access | Use independently qualified MCP clients | T11.1, T11.4, T11.9, T11.13, T11.14 |
| UC-088 | business-extension | Generate transport from business contract | T11.3, T12.1, T12.2, T12.12 |
| UC-089 | business-extension | Compose integrated Go business handlers | T12.1, T12.3 |
| UC-090 | business-extension | Proxy to privately reachable service | T12.1, T12.4, T12.5, T12.6, T12.7, T12.8 |
| UC-091 | business-extension | Reject proxy boundary bypass | T12.4, T12.5, T12.6, T12.8, T15.1, T15.2, T15.3, T15.5 |
| UC-092 | business-extension | Publish fully replaced account UI | T11.12, T12.10, T12.11 |
| UC-093 | business-extension | Preserve side effects across retries | T12.3, T12.7, T12.9 |
| UC-094 | business-extension | Upgrade generated contracts safely | T12.2, T12.12, T14.11 |
| UC-095 | business-extension | Qualify unsupported operation shapes | T11.2, T11.3, T12.2, T12.7, T12.12, T15.5 |
| UC-096 | private-maintenance | Configure owner-hosted maintenance | T13.1, T13.2, T13.6, T13.12, T13.13 |
| UC-097 | private-maintenance | Create bounded diagnosis from observations | T13.1, T13.3, T13.4, T13.13 |
| UC-098 | private-maintenance | Run isolated private coding job | T13.1, T13.2, T13.5, T13.6, T13.7, T13.13, T15.7, T15.11 |
| UC-099 | private-maintenance | Verify independently of proposed patch | T13.1, T13.8, T13.9, T13.13, T15.8, T15.11 |
| UC-100 | private-maintenance | Promote eligible repair automatically | T13.1, T13.4, T13.9, T13.10, T13.11, T13.13, T15.9, T15.13 |
| UC-101 | private-maintenance | Pause, recover and resume safely | T13.4, T13.6, T13.10, T13.11, T13.12, T13.13, T15.12, T15.13, T15.14 |
| UC-102 | private-maintenance | Retain private fixes and evidence | T13.5, T13.7, T13.13 |
| UC-103 | private-maintenance | Continue service during maintenance outage | T13.2, T13.3, T13.12, T13.13, T14.3, T15.14 |
| UC-104 | upstream-maintenance | Inspect and disable sanitized reporting | T14.1, T14.2, T14.3, T14.12, T15.4 |
| UC-105 | upstream-maintenance | Receive bounded sanitized diagnostics | T14.1, T14.2, T14.4, T14.5, T14.12, T14.13, T14.14, T15.5, T15.11 |
| UC-106 | upstream-maintenance | Create public synthetic reproduction | T14.6, T14.9, T14.12, T15.11 |
| UC-107 | upstream-maintenance | Route suspected vulnerabilities privately | T14.4, T14.5, T14.7, T14.12 |
| UC-108 | upstream-maintenance | Repair shared AMOS code independently | T14.6, T14.8, T14.9, T14.12, T15.10 |
| UC-109 | upstream-maintenance | Contribute demonstrated AMSL mechanism gaps | T14.6, T14.10, T14.11, T14.12 |
| UC-110 | upstream-maintenance | Adopt upstream repair through upgrade PR | T14.11, T14.12 |
| UC-111 | upstream-maintenance | Delete or expire reporting data | T14.1, T14.3, T14.4, T14.5, T14.12 |
| UC-112 | security | Review application and maintenance threat boundaries | T12.4, T14.7, T15.1, T15.3, T15.4, T15.15 |
| UC-113 | security | Deny tenant and authority confusion | T11.6, T11.8, T11.11, T12.6, T12.10, T15.1, T15.2, T15.3, T15.15 |
| UC-114 | security | Enforce protected change policy | T13.1, T13.7, T13.8, T13.9, T14.8, T14.9, T15.1, T15.6, T15.7, T15.8, T15.9, T15.11, T15.12, T15.15 |
| UC-115 | security | Verify reproducible release provenance | T13.9, T13.10, T14.9, T15.6, T15.9, T15.10, T15.12, T15.15 |
| UC-116 | security | Treat third-party proposals as untrusted | T14.8, T15.6, T15.7, T15.10, T15.15 |
| UC-117 | security | Withstand injected instructions and exfiltration | T13.5, T13.8, T14.1, T14.2, T14.4, T14.6, T15.1, T15.4, T15.5, T15.7, T15.8, T15.10, T15.11, T15.15 |
| UC-118 | security | Recover compatible data and code state | T13.11, T15.1, T15.12, T15.13, T15.15 |
| UC-119 | security | Pause fleet and qualify recovery authority | T13.12, T15.1, T15.12, T15.14, T15.15 |
| UC-120 | release | Verify a complete paid user journey | T2.5, T16.2, T16.3, T16.6, T16.7 |
| UC-121 | release | Deploy and observe a qualified release | T16.4 |
| UC-122 | release | Recover a released application | T16.5 |
| UC-123 | reference | Rehearse the reference application | T2.6, T2.7, T16.2 |
| UC-124 | release | Adopt a release with accurate support claims | T16.1, T16.6, T16.7, T16.8, T16.9, T16.12 |
| UC-125 | release | Verify autonomous private and upstream repair | T16.10, T16.11 |
