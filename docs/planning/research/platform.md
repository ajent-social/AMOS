# Platform planning research

This is a public planning record. The task input is [platform.json](../inputs/platform.json). No implementation, cloud mutation, release publication or live consumer qualification was performed. Commands in task contracts describe future verification; they are not evidence of completed tests.

## Scope and source of intent

[Vision](../../VISION.md) and [RFC 0001](../../rfc/rfc-0001.md) govern product scope. Both AWS installation profiles are required: managed containers with managed PostgreSQL, and a small VM with the application and PostgreSQL. The installer records the selected profile. Switching an existing installation is a separately approved data migration with a recovery point and traffic cutover, not a configuration toggle.

The platform plan covers E7 initializer/local lifecycle, E8 provisioning, E9 distribution/upgrades, and E10 operations. It inventories UC-050 through UC-079. Go SSR with HTMX/plain JavaScript remains the reference UI; a separate business service still shares the public domain. AWS remains the compute/database target and Cloudflare is required. Detailed profile sizes, egress, proxy behavior and operational targets remain subject to contract freeze.

## Evidence from public AMSL source

The [AMSL bootstrap RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md) separates construction, policy checks, provider preview, live reachability and restore evidence. Candidate source and mocked fixtures cannot establish successful consumer adoption. Upstream security-sensitive changes require maintainer review; no upstream changes are authorized by this plan alone.

The following source findings drive explicit tasks:

| Public source | Observed boundary | Planning consequence |
| --- | --- | --- |
| [containerdeploy](https://github.com/ajent-social/pulumi/tree/main/aws/containerdeploy) | ECS Fargate service; immutable ECR digest; no build/scan or cost-tier selection | Reuse and qualify for managed profile; do not treat as a VM deployer. |
| [privatedatabase](https://github.com/ajent-social/pulumi/tree/main/aws/privatedatabase) | Encrypted private RDS with at least two private subnets, explicit backup/deletion settings and managed master secret | Reuse for managed PostgreSQL; independently exercise application credentials, connectivity and restore. |
| [network](https://github.com/ajent-social/pulumi/tree/main/aws/network) | Two-AZ public/private subnet construction with explicitly optional NAT | Freeze egress deliberately. A private task without NAT/endpoints or another qualified egress path cannot pull its image or send logs. |
| [dnsalias](https://github.com/ajent-social/pulumi/blob/main/cloudflare/dnsalias/dnsalias.go) | DNS-only records; explicit rejection of proxied=true | Cloudflare DNS-only is the proposed initial mode for both profiles. Proxy mode is a distinct qualification proposal. |
| [container-artifact workflow](https://github.com/ajent-social/workflows/blob/main/.github/workflows/container-artifact.yml) | Docker Buildx artifact path | Podman requirement needs explicit AMOS composition or an independently reviewed upstream extension. Do not claim unchanged reuse. |
| [infrastructure-preview workflow](https://github.com/ajent-social/workflows/blob/main/.github/workflows/infrastructure-preview.yml) | AWS OIDC plus Pulumi access-token configuration | Owner-controlled state needs an explicit alternative or upstream extension; preview executes code and must not receive untrusted refs with cloud access. |

Local source inspection supplied these findings; links identify public source locations, not a claim that a particular upstream release is pinned. T1.4 and T8.11 must record the reviewed public dependency revisions and provenance before adoption. Proposed new upstream paths remain proposals. No sibling repository was edited.

## Two precise hosting profiles

Managed is sequenced first for S2 cloud qualification. It composes ECS Fargate, a private RDS PostgreSQL instance, an HTTPS ALB with ACM, and Cloudflare DNS-only records. The contract must choose task size/count, database size/storage/availability tier, subnet/egress design, log retention and backup behavior. Public task IPs, NAT gateways and interface endpoints all have distinct cost/security consequences; no choice is silently inferred. Application service ingress is restricted to the ALB, even if a selected egress profile assigns public task IPs.

Small-VM implementation follows in S3 and has separate S4 evidence. Its proposed reference topology is a Linux EC2 host running Podman application and PostgreSQL services, encrypted retained EBS data storage and a host HTTPS proxy with ACME. It avoids a default NAT or load balancer but has a single-host failure domain and owner patching/database duties. Host replacement must preserve and reattach data storage. It must never format an unrecognized existing volume or expose PostgreSQL to the internet. Exact OS, architecture, sizing and certificate strategy are frozen in T8.1 before execution.

Both expose the same deployment configuration, immutable-artifact, status, backup and restore contracts. They do not share an availability claim. T8.24-T8.25 separately plan and exercise profile migration in both directions, including incompatible PostgreSQL versions/extensions, a write freeze, paused external deliveries, verified isolated restore, traffic cutover, source retention and explicit eventual teardown.

## Authentication, state and provider modes

Local AWS access uses the selected SDK profile/SSO flow; CI uses narrowly scoped OIDC roles. Preview, apply, host runtime, backup and recovery identities have distinct permissions. GitHub recommends binding AWS trust to the expected subject and audience; repository/environment trust must be tested live separately from YAML fixtures. [GitHub AWS OIDC documentation](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws).

Pulumi state is owner-controlled, with an encrypted/versioned S3 backend and independently recoverable secret encryption. Application teardown cannot delete its state foundation or recovery keys. State locking and explicit recovery procedures matter; encrypted state still requires access control. A local nonproduction backend does not establish durable cloud recovery. [Pulumi state and backends](https://www.pulumi.com/docs/iac/concepts/state-and-backends/).

Cloudflare needs a narrowly scoped DNS-edit token for the selected zone, supplied as a secret. Application runtime does not receive it. DNS-only returns the origin endpoint; it does not provide the HTTP proxy's CDN/WAF behavior. Optional proxy qualification separately covers strict origin TLS, direct-origin bypass, forwarding headers, session/private-route caching, webhook delivery and timeouts. [Cloudflare proxy status](https://developers.cloudflare.com/dns/proxy-status/).

No fixed monthly price is asserted. Cost inventories must include selected compute, database, load balancer, NAT/endpoints if any, public IPv4, disks, snapshots/backups, logs, state/KMS, storage requests and transfer. Fargate compute pricing alone excludes several of these items. [AWS Fargate pricing](https://aws.amazon.com/fargate/pricing/).

## Delivery and hybrid ownership

The manifest separates managed generated files, copied-once files, owner-authored business code and declared overrides. Baselines are versioned and retrievable. Upgrade planning performs a three-way comparison of baseline, owner tree and new generated output; conflicts block mutation. Package and workflow pins, migration requirements and custom hook compatibility are part of the plan. The upgrade PR path is idempotent, repository-scoped and has no merge authority.

Public contribution CI is credential-free. Trusted preview, artifact signing/publication and deployment have distinct event/ref/environment requirements. Promotion consumes the exact source-bound digest. Root owns CLI registration, shared schemas, go.mod/go.sum and root workflow wiring; platform lanes own packages and generated fragments. The named scoped tests are created alongside their implementations; root T1.5 supplies the browser wrapper, T1.6 the public artifact checker and T1.8 the Go-check wrapper. None exists by assertion in this research record.

## Operations and qualification boundaries

Liveness and readiness are separate; dependency outages cannot expose secrets through diagnostics. Structured logs use allowlisted fields and metrics use bounded labels. Backups include complete/incomplete status, consistency boundary, schema version and integrity evidence. An isolated restore pauses mail/payment/webhook delivery until reconciliation, checks identity/membership/billing/module state, and measures recovery point/time. Restoring Pulumi state alone does not restore application data.

Minimal shared logical backup and isolated synthetic restore are S2 cloud prerequisites. Scheduled encrypted remote backups, alerts, rotation, compatible rollback, support bundles and full profile recovery drills are S4 work. Logical backup may be inadequate for an owner's eventual RPO/size; managed point-in-time recovery or a separately qualified WAL archive policy must be selected rather than implied. Application rollback does not undo database migrations or external payments.

All production/live acceptance rolls into explicitly authorized E16 qualification. Test doubles stay in test code, and live fixtures must refuse to mutate providers unless the exact target/resources/cost are approved. Small-VM live evidence cannot be borrowed from managed-profile results.

## Scheduling and external lead-time risk

The platform input contains 78 tasks totaling 105.0 estimated engineering/design hours before review, integration or provider waiting time. This is the full product inventory, not one iteration's promise. S1 is 19.5 hours across local lifecycle, CI ownership and health/logging lanes; L09 alone owns 11 hours. The generator/schema/database/authentication seams and one shared integration point constrain elapsed time even with many agents. Later contracts remain provisional until their dependencies supply actual evidence.

Use the local reference as the first releaseable capability: clean generation, durable local database, tested identity/payment development boundaries, visible nonproduction modes, real browser checks and a generated ownership guide. If cloud gates are unresolved, report local qualification accurately and retain the cloud path as blocked; do not simulate a live payment or deployment result.

External lead times are tracked separately from coding estimates. The following are planning allowances, not provider SLAs:

| Gate | Preparation allowance | What can exceed it |
| --- | --- | --- |
| Existing AWS account, scoped roles, Cloudflare token and CI environment | 0.5-2 hours of owner setup | Account enrollment, organizational policy, quota increase or inaccessible administration can take days. |
| Domain already active on Cloudflare | 0.5-2 hours for validation/rehearsal | New registration/delegation, stale DNS or certificate validation can require 24-48 hours or longer. |
| Cost/profile/retention and RPO/RTO choices | 0.5-1 hour of explicit owner decisions | Missing workload/availability requirements prevent a reliable estimate. |
| Artifact registry and protected CI OIDC verification | 0.5-2 hours when access exists | Cross-organization policies or security review are not bounded by coding throughput. |
| Combined live application/payment/restore evidence | A separately scheduled qualification session after prerequisites pass | Payment/account provider approval, DNS/TLS issues or recovery defects can consume the entire session. |

These gates do not authorize spending, publication or provider mutation. Track them independently and prepare reversible local artifacts while waiting.

## Transactional email prerequisite

T8.27-T8.28 provision and qualify the selected T1.11 email adapter independently of the combined release gate. SES is a candidate, not an assumed activated account. Verify sender identity and DKIM in the selected region, preserve existing domain mail policy and scope send permissions to the workload identity. SES sandbox status is regional and restricts recipients to verified identities or its simulator. Production-access review is an external owner action; AWS describes an initial response within 24 hours and possible longer resolution. Simulator acceptance does not prove inbox delivery. A real receipt test requires explicit authorization for the recipient and content. [SES production access](https://docs.aws.amazon.com/ses/latest/dg/request-production-access.html), [SES identities](https://docs.aws.amazon.com/ses/latest/dg/creating-identities.html).
