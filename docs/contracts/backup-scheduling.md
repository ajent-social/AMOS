# Scheduled backup publication and freshness

Proposed amendment v1.34, T10.9. Independent design review and landing precede
source dispatch. This composes accepted T10.7/T10.8 backup source with actual
PostgreSQL scheduling and the separate T10.6 backup signal. It does not qualify
AWS operation, install a scheduler, deliver notifications or establish an RPO.

## Outcome and authority

Provide an executable, bounded, one-shot backup runner and a read-only freshness
reader, plus deterministic invocation assets for both selected hosting profiles.
Neither scheduling nor freshness depends on application requests. The runner calls
the existing `backup.Create` and `Store.Upload` directly; a caller-supplied success
boolean, generic job result, local dump, archive-only upload or duplicate object
cannot advance freshness. Existing backup formats and manifest-last publication
remain unchanged. Application hosts do not start or own this runner.

Exactly one attempt may hold current database authorization for one configured
backup stream. This is **not physical exactly-once execution**: after a database
connection loss, lease expiry, clock jump or network partition, an old process or
S3 request may still finish. PostgreSQL fencing cannot prevent an already-issued
S3 PUT. Fresh attempt object keys and fenced database completion contain this
ambiguity; stale remote objects never independently become freshness authority.
No claim of zero physical overlap, remote rollback or cross-system atomicity.

## Explicit configuration and internal API

Use these exact initial internal composition shapes. Standard library types,
`uuid.UUID`, `opsconfig.Config` and existing `Store` retain their existing meanings.
The integrator owns shared migration/config assembly, not competing lane APIs.

```go
type ScheduleConfig struct {
    Version string
    ScheduleID, InstallationID, ApplicationID uuid.UUID
    Environment string
    Targets opsconfig.Config
    Database ScheduleDatabaseConfig
    AWS ScheduleAWSConfig
    Create ScheduleCreateConfig
    UploadTimeout, RunTimeout time.Duration
    MaxRetainedAttempts, MaxStagingArtifacts uint32
    MaxStagingBytes int64
}
type ScheduleDatabaseConfig struct {
    LedgerWriteRef, LedgerReadRef, DumpRef string
    LedgerWriteRole, LedgerReadRole, DumpRole string
    LedgerHost, LedgerDatabase, LedgerRootCAFile string
    DumpHost, DumpDatabase, DumpRootCAFile string
    LedgerPort, DumpPort uint16
}
type ScheduleAWSConfig struct {
    CredentialKind, AccountID, Region, RuntimeRoleARN string
    BucketAccountID, Bucket, Prefix, KMSKeyARN string
}
type ScheduleCreateConfig struct {
    StagingDir, PGDumpPath, PGDumpVersion string
    ModuleVersions map[string]string
    MaxBytes int64
    DumpTimeout time.Duration
}
type ScheduleResult string
const (
    ScheduleNotDue ScheduleResult = "not_due"
    ScheduleBusy ScheduleResult = "busy"
    ScheduleCompleted ScheduleResult = "completed"
    ScheduleFailed ScheduleResult = "failed"
    ScheduleUnknown ScheduleResult = "unknown"
    ScheduleCapacity ScheduleResult = "capacity"
)
type ScheduleSnapshot struct {
    Version, State, FailureClass string
    ObservedAt, LastAttemptedAt, LastAttemptCompletedAt time.Time
    LastPublishedAt, LastSnapshotAt time.Time
    AttemptDurationMilliseconds uint64
    RetainedAttempts, RetainedLimit uint32
    PublicationKnown bool
}
func NewScheduler(db *sql.DB, cfg ScheduleConfig, store *Store) (*Scheduler, error)
func (*Scheduler) RunDue(context.Context) (ScheduleResult, error)
func (*Scheduler) Snapshot(context.Context) (ScheduleSnapshot, error)
func NewScheduleReader(db *sql.DB, cfg ScheduleConfig) (*ScheduleReader, error)
func (*ScheduleReader) Snapshot(context.Context) (ScheduleSnapshot, error)
```

The CLI version is `amos-backup-config-v1`. Wire field names are the exact
snake_case conversion of these Go field names (`schedule_id`, `ledger_write_ref`,
`runtime_role_arn`, `pg_dump_path`, and so on), with root `database`, `aws`, `create`
and `targets` objects. Targets reuse the strict ops duration-string wire shape.
Other duration fields also use strings. UUIDs are canonical lowercase strings.
Version and every configuration field are required; optional zero/default
credentials or target fields are not accepted. Environment is the existing
`development`/`staging`/`production` string enum, never a new UUID identity.
Snapshot JSON similarly uses exact snake_case names and the status version below;
`PublicationKnown` is derived from a validated ledger read, never accepted as a
configuration input. Finite states are `idle`, `running`, `completed`, `failed`,
`unknown`, `capacity`, `clock_invalid`, `unavailable`. Unknown/malformed output
values are not interpreted as success. Query/identity/validation failures return
generic errors; RunDue's result is not itself freshness authority.

`ScheduleConfig` binds immutable UUIDv7 schedule/installation/application
IDs and existing environment enum; a selected and activated `opsconfig.Config`; explicit AWS runtime account, region,
runtime role and credential kind; explicit backup bucket owner account, bucket,
installation/environment prefix and KMS
references; protected staging directory; the exact `pg_dump` path/version pin;
trusted module versions; an explicit retained-attempt row cap; and explicit
dump/upload/total time and size limits.
The initial runner backs up the full explicitly selected database, not a schema
subset advertised as a full application backup. Existing partial-scope Create
remains usable separately. Dump-reader, ledger-writer and ledger-reader credential
references and authenticated role names are distinct and explicit. Ledger writer/reader target the same
explicit operational database; dump credentials target a separate application
database. Require different database names in this initial profile, even when
both are on the same PostgreSQL server, and verify actual current_database and
authenticated role against each configured target. Role names must differ and
match the corresponding explicit fields. The scheduler writer has only the necessary ledger authority;
the dump reader has only required data/migration reads. Neither silently receives
the other role's grants. An application runtime identity is not a substitute.
No application principal supplies config.

The constructor copies mutable config and validates before acquiring a lease or
performing a dump/upload. It accepts a concrete existing Store, not an arbitrary
completion callback. CLI composition constructs that Store with the dedicated
write-only adapter below. Low-level adapter injection is a composition/test seam,
not evidence that an arbitrary implementation is a live provider.

Use a strict, closed, versioned local JSON configuration, maximum64KiB. Reject
unknown/duplicate decoded keys, aliases, extra/trailing values, invalid reference
syntax and unsupported versions before typed decoding can silently discard them.
Durations use explicit strings, not implicit nanoseconds. BackupInterval is an
integer number of minutes, between1minute and7days; it must satisfy existing
opsconfig validation. An explicit total attempt deadline is positive and at most
1hour. Dump/upload limits are positive and their sum plus15seconds cleanup reserve
must fit that deadline. MaxBytes is positive and at most the existing Store5GiB
limit. Bound modules to64 entries with128-byte versions. Require an explicit
MaxRetainedAttempts between1 and4096 per stream, as described below. Require
MaxStagingArtifacts between1 and4096 and MaxStagingBytes between MaxBytes+2MiB
and1TiB; no default local retention or deletion authority. Do not infer activation
from Defaults, presence of credentials, a generated timer or a nonzero interval.
Unactivated config may render disabled assets, but run fails closed before network
or database access. Status remains a read-only explicitly authorized operation.

Secret-bearing database URLs are resolved only from named bounded `env://NAME`
references selected in protected config; resolved values are never emitted or
stored in schedule/job payloads. AWS key values, bearer values and private key
values are not configuration fields. Protect config/staging ownership and reject
symlink staging directories. Reuse existing Create protection and cancellation
rather than introducing shell interpolation, arbitrary hooks or inherited tool
authentication. Synthetic examples contain placeholders, never selected real
accounts, roles, profiles, buckets, keys, hosts or credentials.

The production runner rejects every ambient `PG*` selector, including empty
values, before opening a database/tool process. Every resolved DSN explicitly
supplies host, numeric port, database, user and authentication material plus
`sslmode=verify-full` and an explicit CA; reject duplicate/unknown query keys,
multi-host/fallback/service/user-home password-file selection and unsupported
authentication routing. Verify each canonical endpoint/database/TLS policy and expected role after
resolution against its explicit LedgerHost/LedgerPort/LedgerDatabase/LedgerRootCAFile
or DumpHost/DumpPort/DumpDatabase/DumpRootCAFile. Ledger read/write credentials
share only the ledger target, never the dump database. These common target fields are nonsecret owner
configuration and part of the immutable digest; require normal TLS hostname
verification and each target's explicit CA. Authenticate the actual session role,
not merely a username string in an unverified URL. No missing field may inherit from
pgx/libpq defaults, HOME/.pgpass, service files or SSL environment. Existing general
Create API remains unchanged; this restriction is at scheduled production
composition. Actual scheduled CLI fixtures use real TLS PostgreSQL and scoped
writer/read roles; no production CLI switch downgrades this to trust/plaintext.

## Runtime AWS identity and write-only adapter

Do not repurpose the deployment `cloudconfig` loader or select its bootstrap,
preview, deploy or recovery roles. Construct the SDK directly from the already
pinned credential packages, with one explicitly selected runtime source:

- `aws_vm` requires `imds_v2`: an owned `imds` client at the SDK's fixed IPv4
  metadata endpoint, with IMDSv1 fallback disabled, feeding `ec2rolecreds`.
- `aws_managed` requires `ecs_task`: `endpointcreds` at the fixed ECS credential
  metadata host plus the platform's `AWS_CONTAINER_CREDENTIALS_RELATIVE_URI`.
  Require a bounded path of the form `/v2/credentials/<single ASCII token>`;
  reject scheme/authority, escapes, traversal, query, fragment and extra segments.
  The suffix is at most128 alphanumeric/hyphen bytes. No full-URI or token-file
  credential endpoint mode in this initial implementation.

Metadata clients have fixed targets, owned transports, no proxy/redirect, bounded
response parsing and a2second request deadline; use at most one SDK attempt.
No DNS-selected metadata host, environment-selected endpoint, caller transport,
IMDSv1, anonymous credentials, shared-profile/default chain or web-identity fallback.
Reject conflicting ambient AWS credential/profile/role/token-file/metadata-endpoint
and service-endpoint override selectors, including empty-valued ones. Proxy and
CA/region environment overrides do not affect owned clients. Region is explicit.
`LoadDefaultConfig` is not an admissible runtime credential selector here.

After durable slot reservation but before Create or any S3 PUT, resolve the
selected runtime credentials and call
regional `sts.GetCallerIdentity` through an owned normally verified HTTPS client,
with one SDK attempt and a2second deadline. Require the configured partition and
account and the exact expected role name in the actual assumed-role ARN; reject
IAM-user/federated-user identities and missing/mismatched results. The initial
expected role ARN must have no IAM path, matching generated host/task role names;
unsupported role paths fail rather than weakening equality. No role assumption,
read/delete privilege or deploy/recovery credential fallback. This network identity
check is not a caller-controlled `verified` flag. Trust policies, platform isolation
and effective resource permissions still need independent provider qualification.

The backup adapter implements the existing `ObjectWriter.PutObject` with the
pinned AWS SDK S3 client. It forwards exact conditional-create, content length,
SHA-256 checksum, metadata and SSE-KMS fields; binds configured region, bucket
owner/account, prefix and key; always set S3 ExpectedBucketOwner from the
explicit backup bucket owner field, never infer it from successful runtime STS.
Validate KMS partition/region/account against the selected destination. Initial
source requires bucket/KMS and runtime accounts to agree; cross-account policy is
unsupported until separately designed. Do not choose a static ACL; returns only actual checksum/version acknowledgements;
and translates provider conflict/failure generically. Missing/null version or wrong
checksum is failure. The adapter performs no Head/Get/Delete/List, cleanup delete,
restore, IAM, KMS administration or credential delegation. Do not reuse
`objects/s3.Store`, whose reconciliation/delete behavior has different authority.
Use ordinary verified regional AWS HTTPS endpoints, no environment proxy/endpoint
or insecure trust override. Each PUT has one SDK attempt within the upload budget;
there is no hidden retry after an ambiguous response. The source initially supports
one selected account/region, not an inferred cross-account recovery arrangement.

Tests may use separately constructed private SDK clients and actual local TLS
metadata/STS/S3-shaped fixtures to qualify parsing, request/ack handling and failure
boundaries. Such fixture clients do not establish EC2/ECS provenance, live S3
behavior or effective IAM/KMS grants; they are not selectable production CLI modes.

## Durable database scheduling and attempts

The integrator owns opt-in `migrations/backup_scheduling.go`, its SQL fragment,
tests and explicit role grants. The initial operational database uses its own
`migrations.NewRegistry(BackupScheduling(1))`. It is separate from the application's
existing complete migration registry (including generated business fragments).
Do not append a new global application migration, modify generated host/default
registries or call application Migrate with a non-prefix subset. The application
database retains its own existing full migration ledger, which Create reads.
Owner-applied creation/migration of the operational database and scoped roles is
an explicit installation prerequisite; run/status/render never create a database,
role, credential or table. Missing/wrong operational migration/checksum fails.
The two databases may share a server, but neither namespace nor credentials are
silently substituted. Operational-ledger recovery is a separate owner recovery
procedure; its loss cannot be repaired by claiming existing remote objects prove
freshness. Use dedicated backup schedule/attempt records, not generic worker-success
aggregates or billing-scoped jobs. The application runtime role has no schedule
mutation or backup credential authority. A dedicated scheduler role can acquire,
observe and finish its bound stream; backup read credentials remain separate.

Persist a unique stream binding for installation/application/environment, its
immutable schedule ID and nonsecret policy/reference digest. Conflicting scope or
policy under an existing ID fails. Hash a deterministic canonical projection of
all nonsecret config, destination and policy fields, with modules sorted. Bind
schedule/scope IDs, profile/activation, targets, interval, limits/cap, binary pin,
staging location, database reference/role names and explicitly configured canonical target identity,
AWS runtime account/role/region, bucket owner/bucket/prefix and KMS reference.
Exclude resolved passwords/keys and ephemeral metadata credential paths, never
exclude a destination or policy field merely to permit a mismatch. Persist only
this digest, not resolved credential material. A digest mismatch cannot insert a
replacement stream, update its policy/profile, rebind credentials or use its lease;
only an explicit separately reviewed operator reconfiguration may replace it.
At bootstrap use a unique stream insert and compare any existing row, not an
upsert that overwrites ownership. All run/status/check paths verify the binding; credential rotation changes resolved material,
not silently the bound reference. Reconfiguration/deactivation is an explicit
operator action, not a background mutation inferred from a changed file. A different
schedule ID cannot bypass the stream's shared exclusion. Status filters exactly
the bound stream and cannot report another installation's success.

In a short actual PostgreSQL transaction, lock the stream row, read fresh
`clock_timestamp()` after the lock, validate due time/current lease, reserve the
slot and commit an immutable new UUIDv7 attempt ID plus an incremented fence and
attempted-at timestamp **before** external work. A uniqueness constraint on
stream/UTC slot prevents repeated invocations from reserving the same slot. IDs
are never recycled. Bound each attempt's stored metadata to4KiB. Maintain a transactionally protected
retained-row count and enforce MaxRetainedAttempts before allocating a new row.
At the cap, deny new admission with finite `capacity` state; do not silently delete
history or stop reporting the previous success's age. No automatic history/object
deletion is added by this task. Owner-approved ledger maintenance may prune terminal
history while preserving the current attempt, latest success, spent-slot high-water
mark and immutable stream/fence identity; it must repair the retained count in the
same transaction. This task does not implement that destructive maintenance path.
S3 retention is not authority to delete database audit history. Status uses indexed
exact-stream/current-attempt/latest-success reads, never an unbounded historical
scan. Finite overflow of fences/counters fails closed.

UTC slots are epoch-aligned integer intervals derived from database time. The
first invocation may run the latest due slot. After missed intervals/restart,
coalesce to at most one latest unreserved due slot, count skipped intervals
numerically and never replay an unbounded backlog. There is one attempt per slot:
a failed, unknown or crashed attempt spends that slot; no inline upload retry or
same-slot replacement. A later due slot allocates a completely fresh attempt ID.
A restart within an already spent slot waits for the next slot. Long-running
attempts exclude later slots while currently authorized; a later invocation
coalesces elapsed slots once authorization has ended. Trigger delivery duplicates
and concurrent processes therefore do not create additional current attempts.

The lease expires at database admission time plus total attempt budget plus15s.
Each external phase checks current scope/attempt/fence/unexpired lease before
starting. During execution a bounded monitor checks that same authorization at
least every5s with queries capped at2s. Any connection/query/authorization failure
cancels the owned attempt context, never grants local authority to continue.
Existing Create cancellation/5s cleanup and an owned context-aware SDK HTTP client
bound cleanup. Explicitly delegate the minimal context-aware Store.Upload local
hashing/read seam: check cancellation between bounded archive-read chunks and
before both PUTs and manifest publication, preserving Store API/formats and all
checksum/version/conditional semantics. Never launch Upload in a detached goroutine
to pretend cancellation completed; no background retry/upload goroutine survives runner completion.
On cancellation terminate the child process, stop the monitor and, on the
supported healthy protected local regular-filesystem profile, qualify return within
15s beyond the total budget. Network/FUSE staging is unsupported; local kernel or
hardware stalls are not universally interruptible by a Go context and are outside
this measured bound, never silently described as guaranteed cleanup. Use bounded cleanup contexts only for cleanup
and fenced failure recording, never to continue remote publication after timeout.
Lease loss can still race a request already sent; the limits above do not promise
instant physical exclusion or remote cancellation certainty.

Only fenced writer transactions persist a database observation watermark, at
admission/authorization checks/completion. Read-only Snapshot never writes a
watermark. Deny admission and mark observations unavailable when current database
time precedes any relevant persisted watermark or stored event time, or a
successfully published snapshot/acknowledgement lies in the future or is reversed.
This detects anomalies against stored authority, not every regression between
independent read-only status calls. Until consistent time returns, do not clamp
negative age to healthy zero or repeat spent slots. A forward jump may expire
authorization and coalesce slots, but cannot let an old fence complete or
manufacture a recent success. Local elapsed duration uses a monotonic clock; it
is not the freshness authority.

## Protected local staging capacity

Before reserving/dumping, inspect the explicitly owned private staging directory
with bounded enumeration (at most configured artifact count plus one), no symlink
following or recursive traversal of unknown entries. Account for current artifact
bytes and reserve MaxBytes+2MiB for the next dump/manifest/temporary connection
files. Require sufficient configured byte/artifact headroom and actual filesystem
free capacity. Foreign/unknown entries or inconsistent ownership are unavailable,
not permission to delete them. Capacity shortage denies the attempt generically
without advancing success. This check cannot prevent unrelated host disk writes;
actual write/space errors still fail the attempt and preserve old freshness.

The runner adds no automatic recursive cleanup of retained completed/crashed
artifacts. Existing Create cleanup of the exact directory it exclusively created
on an ordinary incomplete call is preserved. After a crash, artifacts continue to
count against capacity; an owner must inspect and remove only verified owned
artifacts through a separately approved maintenance procedure. The ledger and S3
retention settings do not authorize local deletion. Rendered runbooks state this
capacity prerequisite and never suggest an unscoped recursive removal command.

## Publication completion and uncertain effects

Create uses the attempt UUID as a new artifact ID. Upload always uses that same
attempt's isolated object keys, first archive and then manifest. Never reuse an
attempt ID after failure, restart, lease loss or unknown acknowledgement. Existing
IfNoneMatch/checksum/version checks stay mandatory. An orphan archive, an already
present object, an ambiguous request result, a missing manifest acknowledgement
or a stale attempt is failed/unknown and cannot be promoted by a boolean or retry.
No read/restore identity is added to repair it opportunistically.

Only after actual Create and Upload succeed may a short database transaction
lock the stream and re-read database time, match scope/attempt/fence and require
unexpired authorization. Then atomically record acknowledged publication completion,
its exact attempt/manifest/archive version references, database publication time,
original successfully published snapshot transaction time and successful attempt duration; release its
lease and advance last success. Validate snapshot time is not in the future
relative to ledger database completion time and does not regress the retained
successful snapshot time. Otherwise deny freshness advancement and expose a
clock anomaly; the remotely written objects remain non-authoritative orphans.
The separate database clocks are not assumed perfectly synchronized: these
checks detect stored/future inconsistencies, not arbitrary undetectable clock
skew or a certified cross-database time source. Failed/late completion leaves last success
unchanged. A process crash after remote publication but before database commit
under-reports success safely: remote completeness alone does not repair the ledger.
The next eligible slot uses a fresh attempt. Do not claim transactionality across
S3 and PostgreSQL or guarantee absence of duplicate physical backup copies.

Failure recording is separately fenced and cannot overwrite a successor attempt.
Persist only finite classes such as `dump`, `upload`, `integrity`, `identity`,
`cancelled`, `lease_lost`, `unknown` or `database`; no raw error or credential text.
A crashed/expired active attempt becomes unknown/abandoned, not successful. At
most the latest attempt and latest successful publication are needed in status;
historical records remain private and do not become metric labels.

## Freshness and the existing alert evaluator

CLI status/check use NewScheduleReader with only the resolved LedgerReadRef and
its dedicated LedgerReadRole (SELECT on bound ledger/status data, no insert,
update, delete or lease operation). They never construct Store, resolve the
LedgerWriteRef/DumpRef, retrieve AWS credentials or call metadata/STS/S3. The full
nonsecret config/reference names are validated and hashed without resolving those
write credentials. Common explicit target fields let the reader verify its own
TLS connection and the stored config/destination digest without granting write
authority. Changing a credential's resolved secret under its existing reference
is rotation; changing its role/reference/target requires explicit reconfiguration.
NewScheduler's Snapshot shares the same read-only query, but is not the CLI's
read-only credential composition. Missing/mismatched grants fail visibly.

Snapshot performs a bounded, read-only, exact-stream database read. Emit closed
JSON `amos-backup-status-v1` with database observation time, finite availability/
active/attempt state, last attempted/completed timestamps, elapsed duration,
finite failure class, last acknowledged publication time and its snapshot time, plus retained row count/cap.
No DSN, path, bucket, role, object/version ID, schedule/person ID or error text in
operator output. Missing stream/success, query failure, inconsistent/future times,
expired observation and inconsistent published snapshot/acknowledgement ordering
are explicit unavailable data, never invented healthy zeroes. Failed/unknown/lost
current attempts do not erase a previous valid successful snapshot: its age keeps
increasing when the read itself is trustworthy. Report blocked/unknown attempt
state separately from that historical freshness, including capacity exhaustion.

The existing T10.6 wording is narrowed by this amendment for the actual adapter:
"successfully completed backup timestamp" means the original data snapshot time
of a successfully published backup, not the time publication finished. Root adds
this clarification and matching runbook wording to dashboard-alerts.md/source;
retain the existing rule ID/measurement name for compatibility. General caller-
supplied samples remain evaluator input, not independently authenticated evidence.

The typed ops adapter derives `Sample.Backup.At` from the fresh database read and
`Value` only from the original snapshot transaction time of the ledger's
successfully acknowledged and fenced publication. Never use dump-finished or
upload/acknowledgement time as the RPO clock. Availability is
derived from that read and validated successful completion, not owner input or
generic jobs/metrics LastSuccess. Do not change `amos-metrics-v1` or silently add
backup labels. T10.6's existing `backup_freshness` rule measures successfully published snapshot/data age against selected RPO;
keep publication acknowledgement time separately visible. Interval
validation or a fresh upload does not prove the data-loss objective is achieved.

`check` produces the actual typed supplemental observation plus a finite direct
fresh/stale/unavailable result against the selected target. Existing alert bundle
evaluation/hold semantics remain authoritative when evaluating a chronological
sample series. A one-shot check is not a persistent incident monitor. Notification
sending remains disabled; no new sender, live monitoring service or continuous
recovery claim follows from a generated asset or synthetic sample.

## CLI and hosting assets

Provide actual `amos backup run --config <file>`, `amos backup status --config
<file>`, `amos backup check --config <file>` and `amos backup render --config
<file>`. Run invokes RunDue once, never a web request loop. Status/check read the
ledger and evaluate freshness without dumping/uploading. Render validates local
config and writes a deterministic bounded bundle without resolving credentials,
contacting AWS/PostgreSQL, installing units or mutating files: the bundle is written only to stdout. Invalid input and
runtime failure return generic nonzero diagnostics without echoing supplied data.
Not-due/already-authorized runs are explicit finite no-op results.

VM assets are systemd oneshot service/timer definitions invoking the actual pinned
AMOS executable/config and a separate periodic check, with bounded service timeout,
private environment references and no shell interpolation. Managed assets are an
explicit Go/Pulumi EventBridge Scheduler→ECS RunTask recipe with selected immutable
task/image/config references, task/execution/scheduler roles, cluster, private
subnets/security groups and region. Disable flexible delivery windows and scheduler
retries; database slot reservation handles duplicate delivery. The task runs the
same executable with its separate runtime credential mode. A separate scheduled
check uses read-only authority. Task role/pass-role scopes and network settings
must be explicit; do not grant a scheduler deployment or backup bucket creation.
Generated AWS/Pulumi/mock evidence does not qualify actual trigger delivery.

Both profiles wake the runner/check every minute; the durable database slot gate
owns the configured backup interval, so platform retry/restart cannot bypass it.
Render files are at most64KiB in total and include version/profile/activation and
explicit install/qualification limits. Use typed generation and reject unsafe
unit/resource identifiers rather than shell/template interpolation. Inactive assets
remain disabled. Hosting installation, enabling tasks and real spending require
separate owner authority; this source task performs none of those actions.

## Delegation and required proof

T10.9's original schedule.go/tests and backup-timer templates cannot independently
qualify this outcome. Integrator explicitly delegates directly necessary new
`internal/backup/schedule*.go`, write-only AWS adapter/runtime credential source and
tests; minimal context-aware existing store.go hashing/read changes plus tests; `cmd/amos/backup.go`/tests and minimal dispatch; finite ops supplemental
adapter/tests and scheduling assets; and managed Go/Pulumi schedule recipe/tests.
Root retains contracts/task/registry, module manifests, migration ordering/registry
and role grant wiring. No dependency versions, application host behavior or
existing backup/store formats change. Root owns the opt-in operational registry/fragment and chooses any remaining
shared source/role-grant paths before implementation. Independent review/landing of this design precedes source.

Require actual PostgreSQL tests of concurrent processes/instances, row locks,
scoped status isolation, immutable policy conflicts, lease/fence loss, duration,
backward/forward clocks, long jobs, duplicate triggers, spent slots, missed slots
and restart. Simulate clock observations through a private finite test seam while
still exercising real lease/ledger SQL; do not claim a changed system clock or
provider timing from those cases. Run actual pinned pg_dump against a migrated
fixture and upload its actual bytes through the dedicated SDK adapter to a real
local TLS S3-shaped server. Assert both checksums, distinct keys, conditions,
manifest-last order, version acknowledgements, wrong identity/CA/account/role,
absent/incorrect checksums/null versions, partial upload, ambiguity and no forbidden
read/delete/list calls. Assert explicit TLS PostgreSQL credentials and grants,
ambient PG selector rejection, bounded ledger/staging capacity and digest mismatch
fail-closed behavior. Bound actual process/network cancellation and cleanup, including actual
pre-upload archive hashing cancellation with an already-created local artifact;
prove no PUT follows that cancellation. State the filesystem fixture and measured
limit without claiming a universal kernel-I/O deadline.

Exercise the actual CLI, both generated profiles and actual database-derived
backup alert input. Repeated upload/manifest failures must retain the old success
and let its successfully published snapshot age into stale/firing under the generated rule; a new fenced complete
publication enables fresh/recovery only under existing holds. Unknown/stale reads
cannot recover an incident. Prove late remote completion and duplicate-ID responses
do not advance the ledger. A compiled isolated mutation advancing last success on
failed upload must make this regression RED, then exact restoration GREEN.

Report missing pg_dump/database/TLS fixtures as failed prerequisites, never skipped
passes. Obtain source/race/vet/lint/privacy checks, independent exact-head review,
guarded merge and fresh landed verification. Local fixtures qualify only source,
SQL and wire behavior. Live runtime identity, IAM/KMS, encrypted/versioned S3,
retention, installed VM/ECS scheduling, host-loss durability, actual restore and
RPO/RTO achievement remain separate owner/provider/E16 gates.
