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

Reference region is **US East (N. Virginia), `us-east-1`**. This is a comparison workload, not a forecast of any owner's deployment: 730 hours/month; two continuously running Fargate tasks each at 0.5 vCPU/1 GiB; `db.t4g.medium` PostgreSQL Multi-AZ with 100 GiB gp3; 100 GiB retained application data; two AZ-local NAT Gateways processing 200 GB/month; one ALB averaging 0.5 LCU; 200 GB/month Internet egress; 5 GB/month application log ingestion and 5 GB-month retained; and 1 GB-month of S3 state. VM comparison is one `t4g.medium` for 730 hours, 100 GiB encrypted gp3, 100 GiB-month retained EBS snapshots, one public IPv4, the same 200 GB Internet egress, 5 GB logs ingested/retained and 1 GB-month S3 state. No free-tier allowance, credits, reservations, savings plans, taxes, support, Cloudflare plan, domain, cross-region copy, malware scanning, or CPU surplus credits are included.

Rates below are AWS published list rates checked 2026-10-01; service calculators are authoritative for an actual selected region, platform, and configuration. EC2 and RDS hourly rate cells are intentionally calculator inputs because official interactive pricing varies by operating system/license and RDS engine/availability mode. Do not treat a third-party instance-price index as an AWS quote.

| Cost category | Managed reference | VM reference |
|---|---:|---:|
| Compute | Fargate x86 Linux: 2 tasks × (0.5 vCPU × $0.000011244 + 1 GiB × $0.000001235) × 2,628,000 seconds = **$36.03**. | `t4g.medium` Linux on-demand hourly rate × 730 h (enter current `us-east-1` rate in [EC2 On-Demand pricing](https://aws.amazon.com/ec2/pricing/on-demand/)); exclude T4g surplus credits. |
| Database | `db.t4g.medium` PostgreSQL Multi-AZ hourly rate × 730 h (use [RDS PostgreSQL pricing](https://aws.amazon.com/rds/postgresql/pricing/)); CPU surplus credits may add cost. | PostgreSQL uses the same host compute line; no separate RDS charge. |
| Disks | 100 GiB RDS gp3 × current regional GB-month rate plus any provisioned performance above included baseline (see [RDS storage](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/CHAP_Storage.html)). | 100 GiB gp3 × $0.08/GB-month = **$8.00**, plus root-volume capacity if separate. gp3 baseline includes 3,000 IOPS and 125 MB/s. |
| Public IPv4 and ingress | ALB public IPv4: 2 addresses × $0.005 × 730 = **$7.30**. | 1 in-use public IPv4 × $0.005 × 730 = **$3.65**. |
| Load balancing | ALB fixed $0.0225/h × 730 = **$16.43**; 0.5 LCU × $0.008/h × 730 = **$2.92**. | No ALB in the reference design; host serves HTTPS directly. |
| Egress | NAT hourly: 2 × $0.045 × 730 = **$65.70**; NAT processing: 200 GB × $0.045 = **$9.00**; public Internet transfer: 200 GB, estimated **$9.00** after the first 100 GB free allowance. | Public Internet transfer: 200 GB, estimated **$9.00** after the first 100 GB free allowance. |
| Backups | RDS backup storage up to allocated database storage is included while the instance is active; excess retained backup GB-month × current region rate, plus any cross-region copy. Model excess separately when actual retention is chosen. | 100 GB-month EBS snapshot storage × current regional snapshot rate. This is variable with changed blocks; 100 GB is a scenario input, not an assertion about snapshot size. |
| Logs | 5 GB application logs × $0.50/GB ingest = **$2.50**, plus retained log GB-month × current storage rate. | Same: **$2.50** ingestion, plus retention storage. |
| State | 1 GB S3 Standard × current region GB-month rate (see [S3 pricing](https://aws.amazon.com/s3/pricing/)), plus requests/version storage. | Same. |

The numeric subtotal omits calculator-dependent EC2/RDS, storage, backup, CloudWatch retention and S3 state values above; it is therefore **not a complete monthly total**. Add the missing cells from the linked AWS calculators for the exact workload before comparing. Prices can change and workload variation dominates several lines. The included formulas make fixed published inputs reproducible; they do not set a budget. AWS defines NAT at $0.045/hour plus data processing, public IPv4 at $0.005/address-hour, gp3 examples at $0.08/GB-month, ALB at $0.0225/hour plus LCU, and CloudWatch application log ingestion at $0.50/GB. [VPC pricing](https://aws.amazon.com/vpc/pricing/), [EBS pricing](https://aws.amazon.com/ebs/pricing/), [ELB pricing](https://aws.amazon.com/elasticloadbalancing/pricing/), [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/).

## Estimate completeness check (negative fixture)

A review estimate must have separate entries for `compute`, `database`, `disks`, `public_ipv4`, `load_balancing`, `egress`, `backups`, `logs`, and `state`. A zero-cost line is valid only when its rationale is recorded. This intentionally incomplete fixture is rejected because it omits public IPv4 and backups (as well as other required categories):

```yaml
estimate:
  compute: 36.03
  database: calculator-required
  disks: calculator-required
  egress: 18.00
  logs: 2.50
```

The review check is `missing = required_categories - estimate.keys()`; reject when `missing` is non-empty. Applying it to this fixture must report missing `backups`, `load_balancing`, `public_ipv4`, and `state`. This is a documentation completeness check only; it does not qualify a deployed environment or provider.
