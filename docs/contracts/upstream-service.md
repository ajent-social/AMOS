# Isolated upstream service configuration v1

Status: proposed T14.13 design for independent review and integrator freeze.
`amos-upstream-config-v1` describes desired deployment boundaries. No resources,
collector, intake, credentials, sanitization, access enforcement, retention job,
source provenance, consent or transmission are implemented or qualified here.
A configuration passing this validator is not effective isolation evidence.

## Separate deployment and authority

Use a separately operated upstream deployment with the existing `aws_managed`
or `aws_vm` profile mechanisms. Do not copy a consumer's generated app deployment,
Pulumi state, database credentials, session keys or IAM roles. There are no
real account/identity/resource defaults. `DefaultCollection` returns disabled
limits only; a complete explicit configuration is still required. Fixture
accounts/roles/keys/bucket names are synthetic and are not selected resources.

The upstream declares its own AWS account, commercial AWS region, identity
namespace `amos-upstream-operators`, storage namespace `amos-upstream-diagnostics`,
bucket reference and KMS reference. Four distinct declared purposes are operator,
intake, storage and deployment. ARN account, domain/path and purpose must agree
with the corresponding typed declaration; a different ARN string alone is
insufficient. Upstream roles use `amos-upstream/<purpose>/...`; consumer deploy
roles use `amos-consumer/deploy/...`. Every declared consumer account must differ
from the upstream account, even when its role ARN is different. Consumer
credentials cannot become upstream operator or service credentials by relabeling
an `account_id`, `domain` or `purpose` field. Duplicate consumer accounts fail.
KMS account/region, storage account and abuse-secret account must match upstream.

The `consumers` list is a bounded declared exclusion inventory, not evidence that
all actual consumers have been discovered. Before deployment, an authoritative
organization/installation inventory must establish its completeness. No claim of
universal separation follows from an arbitrary caller-supplied list. This initial
profile does not support same-account consumer/upstream hosting or noncommercial
AWS partitions; no weaker fallback is selected.

Effective isolation additionally requires independently qualified IAM principal
and trust identity, role policies/permissions boundaries, cross-account grants,
state ownership, KMS policy, bucket ownership/public-access policy, database
roles, network/TLS and operator authentication. Lexical ARN namespaces, declared
account differences, a matching digest or a caller `isolated` boolean cannot
establish those facts. Unknown credential/approval/trust fields are rejected.
Actual AWS or secret-provider authorization must be verified by a separately
approved deployment task; this validator neither reads secrets nor calls AWS.

Operator human inspection is private and authenticated, with MFA and narrowly
scoped upstream-only authorization. Deployment cannot read quarantine records or
change operator membership through ordinary application authority. Intake may
write normalized quarantine records but cannot read/export them or create jobs.
Storage maintenance has only bounded retention/deletion duties. These are
normative future policy requirements; the named ARN is not proof they are met.

## Intake, privacy and quarantine budgets

Collection defaults off. `mode=enabled` is an explicit requested setting only;
it neither grants deployment authority nor proves owner approval or runtime
readiness. Future activation must independently qualify enforcement and owner
control before exposing an endpoint. A separately approved upstream origin and
TLS boundary are required; do not reuse a consumer's application origin/login.
No automatic intake or outbound transmission is introduced by this manifest.

Hard ceilings: decoded and compressed request bodies each 8,192 bytes, five-second
request deadline, four concurrent requests, and 64 accepted reports per rolling
24 hours globally. Count all attempts against abuse budgets, including invalid
and repeated requests; accepted reports additionally consume storage capacity.
Anonymous intake allows at most one attempt per derived source per 24 hours,
16 globally, and 256 simultaneously retained derived-source budget entries.
Enrolled intake allows at most one attempt per authenticated enrolled principal
per 24 hours, 64 globally, and 1,024 retained principal budget entries. Anonymous
plus enrolled global allocations must fit the shared global ceiling. A channel
is disabled with both its per-principal and global budget zero. Budget identity
must not reset on retry, reenrollment, process restart or clock rollback.

Proposed defaults are two concurrent requests, 32 total daily attempts split
8 anonymous/24 enrolled, and 128 anonymous/1,024 enrolled budget entries.
Anonymous source keys are daily HMACs with a separately scoped upstream secret
reference, not stored raw IPs or caller-selected identities. Keep budget state
bounded; deny on unavailable/unknown/reset state or capacity exhaustion. Enrollment
requires future authenticated upstream enrollment state; a sender-supplied label
never grants the enrolled allowance. No raw enrollment credential or secret value
belongs in this configuration. The declared reference is not availability proof.

Only payloads validated and freshly normalized under the frozen public diagnostic
profile may enter private quarantine. `normalized-quarantine-v1`, `operator-only`
read access, `discard` raw-payload policy and KMS encryption are mandatory desired
settings. Invalid/raw compressed bodies, source, logs and request headers must
never be persisted, including rejection logs, queues, traces, dead letters or
backups. If bounded rejection metadata is retained, it contains only approved
category/digest fields; no raw diagnostic may accompany it. Decompress with an
absolute output cap before parsing; no compressed-bomb buffer or diagnostic-driven
URL fetch/command is allowed. Valid-shaped reports remain untrusted data and
cannot create trusted jobs, public issues or releases.

Retention is at most seven days (168 hours), with lower operator-selected limits.
Storage has hard ceilings of 1,024 records and 8 MiB; defaults are 128 records and
1 MiB. Full storage, unavailable normalization, deadline expiry or failed retention
must deny intake rather than spill to public/raw storage. Future retention must
cover quarantine, queues, replicas and backups; expired data cannot reappear on
restore. Access/transport logs need a bounded independently reviewed policy and
may not contain payloads or extend payload retention. No anonymity guarantee is
made: transport timing/address correlation and rare aggregates still fingerprint.

## Validation and qualification boundary

The Draft 2020-12 schema closes every nested object and bounds scalar/array fields.
The Go validator additionally rejects missing/unknown/case-aliased/duplicate
decoded keys, invalid encodings, oversized/deep configurations, integer exponent
forms, cross-field account/region/purpose mismatches, duplicate consumer accounts
and inconsistent budget sums/disablement. These relational and wire checks are
mandatory alongside schema validation; JSON Schema alone cannot express all of
them. In particular the shared-account rejection fixture is schema-valid but
fails the finite profile validator.

Independent schema meta/fixture checks and Go design tests do not qualify actual
IAM, cloud storage, secret availability, network isolation or intake behavior.
T14.4 owns real quarantined intake; E16 owns applicable live/deployed evidence.
No AMSL revision is selected or copied. Changes to identity/storage semantics or
budget ceilings require independent review and integrator version amendment;
operators may lower limits but cannot add raw diagnostic fields or bypass gates.
