# AWS reference profiles

Status: proposed reference design for review; this is not an implementation approval, deployment record, or spending authorization.

## Stable selection and outputs

The immutable machine identifiers are `aws_managed` and `aws_vm`. “Managed” and “small VM” are display descriptions only. Persist the selected identifier in both application configuration and stack metadata at creation. A profile change is a planned migration: export and verify state, provision the target, restore and validate, switch DNS, observe, then retire the source only after explicit owner approval. Do not infer a migration from a changed default.

Both profiles produce the same application-facing record: profile ID, deployment/stack reference, public HTTPS origin and health/readiness paths, immutable application artifact digest, database endpoint and backup/restore reference, state artifact reference, Cloudflare DNS record reference, and timestamped operation result. Secrets are references to owner-managed secret storage and are never emitted in outputs. Outputs contain no credential, private key, database password, or raw state.

The profile owner supplies the AWS account, region, Cloudflare zone and credentials, domain, artifact, workload limits, retention, and budget. These documents do not select or create an account, domain, IAM grant, or real resource. Cloudflare DNS is required for either AWS profile; DNS proxying and provider qualification remain governed by their separate contracts.

## `aws_managed`

**Reference shape:** one VPC across two Availability Zones; public subnets contain an internet-facing HTTPS Application Load Balancer (ALB); private subnets contain ECS Fargate application tasks and a private Amazon RDS for PostgreSQL Multi-AZ instance. ALB terminates TLS using an owner-selected certificate. Cloudflare DNS points the owner-selected hostname to the ALB. RDS has no public address and accepts PostgreSQL only from the application security group. ALB accepts HTTPS from the internet; task ingress accepts only ALB traffic. Tasks have no inbound public addresses.

**Egress decision:** route private task egress through one NAT Gateway per AZ, with same-AZ routes and an S3 gateway endpoint where needed. This preserves private task addresses and supports arbitrary external HTTPS services required by the application. NAT hourly, per-GB processing, public IPv4, and Internet data transfer are separate charges. A VPC endpoint-only design can lower NAT cost for AWS services that have endpoints but does not provide general Internet access; every needed service must be inventoried and some interface endpoints also have hourly and data charges. Public tasks can avoid NAT but expose task public addresses and increase the inbound exposure surface. Neither alternative is silently substituted for the reference topology.

**Availability and operations:** ALB and tasks span two AZs; RDS Multi-AZ supports standby failover. This is not a guarantee of application availability. The owner operates deploy/rollback, health checks, capacity limits, database migrations, alarms, incident response, and Cloudflare changes. Backups use RDS automated backups plus scheduled snapshots/copies according to an owner-set retention and recovery-point objective; test restores are required. Logs go to CloudWatch with an owner-set retention period and no secrets/PII by default. State artifacts go to a private, versioned, encrypted S3 bucket with lifecycle and deletion protection controlled by the owner.

**Deletion:** removing application tasks/ALB/network is an explicit teardown. RDS deletion protection remains on until a deliberate data-retirement step; final snapshot is retained by default. State bucket versions and database backups are retained independently of stack deletion. The owner approves retention expiry and data destruction. DNS removal follows traffic migration and owner approval.

## `aws_vm`

**Reference shape:** one EC2 Graviton `t4g.medium` (2 vCPU, 4 GiB), ARM64 Linux, Podman, and owner-hosted PostgreSQL on the same host bound to loopback or a private socket. A 100 GiB encrypted gp3 EBS data volume holds PostgreSQL and durable application data; the root disk is encrypted too. The instance serves HTTPS directly through host-managed TLS (for example, Caddy with ACME). Cloudflare DNS points the owner hostname to the instance's Elastic IP. Security group ingress is limited to HTTPS and an owner-approved administrative path; PostgreSQL is never open to the Internet. There is one public IPv4 address. No ALB or NAT Gateway is implied.

**Availability and operations:** this is a single-AZ single-host design. Host, AZ, disk, operating-system, container runtime, PostgreSQL, TLS renewal, patching, firewall, monitoring, backups, and recovery are owner responsibilities. Use encrypted EBS snapshots plus application-consistent PostgreSQL dumps or a tested backup tool; EBS snapshots alone are crash-consistent. Retain the encrypted data volume when deleting/replacing the instance, and retain backups in a separately controlled encrypted bucket. Logs and state artifacts use the same owner-controlled CloudWatch/S3 controls as above. A failed host or AZ causes an outage until owner recovery. Adding an ALB, standby database, NAT, or second host is a separately selected topology with extra cost and operations.

**Deletion:** instance termination must not delete the retained data volume or backup objects. Deletion of EBS snapshots, database backups, state versions, or DNS is a separate owner-approved retention action. Preserve a final verified backup before replacing the host.

## Current illustrative price model

Reference region is **US East (N. Virginia), `us-east-1`**. This is a comparison workload, not a forecast of any owner’s deployment: 730 hours/month; two continuously running Fargate x86 Linux tasks each at 0.5 vCPU/1 GiB; `db.t4g.medium` PostgreSQL Multi-AZ, 100 GB GP3, 7-day point-in-time retention; 100 GB-month of RDS backup storage (within its 100 GB included allowance); 100 GB retained app data; two AZ-local NAT Gateways processing 200 GB/month; two public ALB IPv4 addresses and two NAT IPv4 addresses; one ALB averaging 0.5 LCU; 200 GB/month total Internet data transfer out across NAT-routed dependencies and application responses; 5 GB/month log ingestion and 5 GB-month billable log retention; 1 GB-month S3 state plus 1,000 PUT and 1,000 GET requests. VM comparison is one ARM64 Linux `t4g.medium` for 730 hours, 20 GB root plus 100 GB encrypted GP3, one public IPv4, 200 GB Internet egress, one 120 GB standard snapshot retained for a month, a 100 GB PostgreSQL dump in S3 Standard, the same log assumptions, and the same S3 state assumptions. For S3, costs use decimal GB; for EBS and RDS provisioned volumes, GB is the AWS billed unit. No free-tier allowance, credits, reservations, savings plans, taxes, support, Cloudflare plan, domain, cross-region copies, or CPU surplus credits are included. Internet transfer charges are conservatively calculated on all 200 GB, before any account-level allowance.

Rates were extracted on 2026-10-01 from AWS’s public regional Bulk Price List JSON for `us-east-1` (publication dates: EC2 2026-09-25, RDS 2026-10-01, S3 2026-09-28, CloudWatch 2026-09-22, data transfer 2026-09-16, and VPC 2026-09-17) (official inputs: [EC2](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/us-east-1/index.json), [RDS](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonRDS/current/us-east-1/index.json), [S3](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/us-east-1/index.json), [CloudWatch](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonCloudWatch/current/us-east-1/index.json), and [data transfer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AWSDataTransfer/current/us-east-1/index.json), and [VPC](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonVPC/current/us-east-1/index.json)). These are current on-demand public list rates, not negotiated rates. Fargate and ALB rates use the [AWS Fargate](https://aws.amazon.com/fargate/pricing/) and [Elastic Load Balancing](https://aws.amazon.com/elasticloadbalancing/pricing/) pricing pages. They can change; rerun the same SKU attribute selections against the live regional offers before a real estimate.

| Cost category | Managed reference | VM reference |
|---|---:|---:|
| Compute | Fargate Linux/x86: 2 × (0.5 vCPU × $0.000011244 + 1 GiB × $0.000001235) × 2,628,000 seconds = **$36.04**. | `t4g.medium` Linux: $0.0336/h × 730 h = **$24.53**. |
| Database | RDS PostgreSQL `db.t4g.medium` Multi-AZ: $0.129/h × 730 h = **$94.17**. | PostgreSQL runs on the EC2 host; no separate RDS service. |
| Disks | RDS GP3 Multi-AZ: 100 GB × $0.23/GB-month = **$23.00**. | GP3: (20 GB root + 100 GB data) × $0.08/GB-month = **$9.60**. |
| Public IPv4 and ingress | 2 ALB addresses + 2 NAT addresses: 4 × $0.005/address-hour × 730 h = **$14.60**. Address count is an estimate; ALB scaling can change it. | 1 in-use public IPv4 × $0.005/address-hour × 730 h = **$3.65**. |
| Load balancing | ALB: $0.0225/h × 730 h = $16.43; 0.5 LCU × $0.008/h × 730 h = $2.92; **$19.35** total. | No ALB in this topology; HTTPS terminates on the host. |
| Egress | NAT Gateway hours: 2 × $0.045/h × 730 h = $65.70; NAT processing: 200 GB × $0.045/GB = $9.00; Internet data transfer out across NAT-routed dependencies and application responses: 200 GB × $0.09/GB = $18.00; **$92.70** total. | Internet data transfer out: 200 GB × $0.09/GB = **$18.00**. |
| Backups | 100 GB-month RDS backup usage within the included allowance (up to allocated database storage while active) = **$0.00**; excess is $0.095/GB-month. | 120 GB-month standard EBS snapshot scenario × $0.05/GB-month = $6.00; 100 GB-month PostgreSQL dump in S3 Standard × $0.023/GB-month = $2.30; **$8.30** total. Snapshot bytes are modeled as a conservative retained full-size point, although actual snapshots are incremental and depend on changed blocks. |
| Logs | Ingest 5 GB × $0.50/GB = $2.50; retained billable 5 GB-month × $0.03/GB-month = $0.15; **$2.65** total. | Same: **$2.65**. |
| State | S3 Standard: 1 GB × $0.023/GB-month + 1,000 PUT × $0.005/1,000 + 1,000 GET × $0.004/10,000 = **$0.0284**. | Same: **$0.0284**. |
| **Illustrative monthly total** | **$282.53** | **$66.76** |

Managed total sums the displayed rounded line items using unrounded arithmetic: $36.040392 + $94.17 + $23 + $14.60 + $19.345 + $92.70 + $0 + $2.65 + $0.0284 = $282.533792, displayed as **$282.53**. VM total is $24.528 + $9.60 + $3.65 + $18 + $8.30 + $2.65 + $0.0284 = $66.7564, displayed as **$66.76**. The table’s rounded total cells should use these rounded totals. (The row subtotals may differ by a cent from arithmetic on printed two-decimal amounts.)

The log storage input means 5 GB of billable compressed bytes retained for one month. RDS backup storage at or below the allocated database storage is included while the DB instance is active; the estimate explicitly models only 100 GB-month within that allowance. EC2 transfer out uses the first tier’s $0.09/GB rate and ignores account-wide free transfer allowance. NAT charges and Internet transfer are separate. S3 request counts are illustrative low-volume assumptions. The figures exclude taxes, support, application-specific data volume, DNS/domain, cross-AZ application/database data transfer, alarms/metrics, interface endpoints, CloudTrail/VPC flow logs, image registry storage/transfer, operating effort, and burst-credit surplus. This total is a reproducible comparison scenario, not an owner’s budget or a quote.

## Estimate completeness check (negative fixture)

A review estimate must have separate entries for `compute`, `database`, `disks`, `public_ipv4`, `load_balancing`, `egress`, `backups`, `logs`, and `state`. A zero-cost line is valid only when its rationale is recorded. This intentionally incomplete fixture is rejected because it omits public IPv4 and backups (as well as other required categories):

```yaml
estimate:
  compute: 36.04
  database: 94.17
  disks: 23.00
  egress: 92.70
  logs: 2.65
```

The review check is `missing = required_categories - estimate.keys()`; reject when `missing` is non-empty. Applying it to this fixture must report missing `backups`, `load_balancing`, `public_ipv4`, and `state`. This is a documentation completeness check only; it does not qualify a deployed environment or provider.
