# Proposed decision: two AWS reference profiles

Status: proposed; requires integrator review and acceptance. No AWS account, domain, IAM permissions, or spending is authorized by this proposal.

## Decision

Keep the canonical machine identifiers `aws_managed` and `aws_vm`, immutable in application and stack metadata. Treat “managed” and “small VM” as display text only. Require an explicit profile selection and explicit owner-supplied region and workload. A profile change is an explicit migration with state and data verification, DNS cutover, observation, and separately approved source retirement.

Adopt two reference topologies:

- `aws_managed`: ECS Fargate behind a public HTTPS ALB, private Multi-AZ RDS PostgreSQL, private task subnets across two AZs, NAT Gateway in each AZ, Cloudflare DNS.
- `aws_vm`: one ARM64 EC2 `t4g.medium` running Podman and PostgreSQL locally, encrypted retained EBS, host-terminated HTTPS, one public IPv4 and Cloudflare DNS. No ALB or NAT Gateway unless separately selected.

For managed tasks, choose same-AZ NAT egress as the reference because it keeps task addresses private while supporting non-AWS Internet dependencies. Use gateway/interface endpoints selectively when their complete endpoint and traffic cost is lower. Endpoint-only egress is unsuitable where an external HTTPS dependency is required. Public task egress reduces NAT fixed cost but exposes task addresses and broadens the security surface; it is not the default.

## Consequences and ownership

The managed profile reduces host and database operations but adds service and network cost. Two AZs and Multi-AZ database do not guarantee application availability; deployment, health checks, restore tests, capacity, incident response, and DNS remain owner work. The VM lowers managed-service count and has simpler direct ingress, but host, operating system, containers, TLS, PostgreSQL, backups, patching, monitoring, and recovery remain owner work; the single-AZ host is an explicit outage concentration.

For both profiles, the owner owns accounts, permissions, secrets, region, Cloudflare zone, domain, workload and limits, retention, backup recovery objectives, and spending decisions. State and backups must be encrypted, versioned/retained under owner controls, and independently deleted. Outputs expose references and operation evidence but never secrets. Destructive retirement of database, volume, state versions, backups, or DNS is a separate owner-approved step.

The `us-east-1` comparison in [the profile contract](../contracts/aws-reference-profile.md) uses a named hypothetical workload and current official AWS price-list rates for compute, database, disks, IPv4, load balancing, egress, backups, logs, and state. Its illustrative monthly totals are $282.53 for `aws_managed` and $66.76 for `aws_vm`, under the stated assumptions and exclusions. Estimates are not an owner budget, deployment evidence, or provider qualification.

## Review controls

- Reject non-canonical machine IDs and reject mutable profile changes without explicit migration metadata.
- Reject estimates missing any of: compute, database, disks, public IPv4, load balancing, egress, backups, logs, or state. The contract includes a deliberately incomplete estimate fixture missing IPv4/backups and documents the rejection rule.
- Do not create resources or broaden permissions under this proposal. The accepted contract must be reviewed against the existing canonical configuration and AWS/Cloudflare provider contracts before implementation.
