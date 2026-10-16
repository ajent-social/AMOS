# Scheduled backup publication source checks

T10.9, adopted contract v1.35. Source is independently approved; guarded source
landing and fresh landed verification remain pending. Task acceptance stays in
progress until those gates complete. No scheduler installation, live AWS behavior,
production operation, remote restore or advertised RPO is qualified here.

The standalone `amos backup run/status/check/render` path composes the existing
actual database dump and manifest-last Store. A separate operational PostgreSQL
database uses opt-in `migrations.BackupScheduling(1)` and owner-applied scoped
writer/reader grants. Commands do not create databases, roles or migrations, and
the application/default migration registry is unchanged. Runtime identities,
TLS targets, environment references, staging capacity and publication destinations
are explicit; no application-request traffic drives scheduling.

Database-clock slots, immutable attempt IDs, scope/config binding and monotonic
fences admit one currently authorized attempt. Only archive plus manifest
acknowledgements followed by current fenced completion advance success. PostgreSQL
cannot retract an already-issued remote request during partition or lease loss:
physical exactly-once execution and zero physical overlap are not promised.
Failed, partial, duplicate, uncertain or stale attempts cannot manufacture freshness.
The read-only signal uses the successfully published snapshot's data time, with
acknowledgement time retained separately. Retention caps fail closed; no automatic
history, local data or remote-object deletion is added.

The runtime adapter selects fixed IMDSv2 or bounded ECS task metadata, verifies
STS account/assumed-role identity and uses the identical captured expiring
credential set for S3. No refresh/default chain/static/profile fallback, endpoint
or trust override establishes runtime authority. Conditional one-attempt writes
check exact destination, length, checksum and version acknowledgements. They gain
no read/list/delete authority. The pinned IMDS provider caps expiry at one hour;
a one-hour attempt plus cleanup therefore fails the lifetime check rather than
weakening it. Reader commands do not resolve AWS or write/dump credentials.

## Observed checks

The exact integrated candidate `38749d57400ca2b5af912b978e8ef8c2daf69c5d` passed
210 named Go1.27.1 race checks, zero failures/skips, across full affected packages:
`internal/backup`, `cmd/amos`, `infra/backupschedule`, `internal/scaffold/ops`
and `migrations`. A separate actual host readiness/schema regression passed one
named race check. All five affected packages passed vet and pinned lint with
zero issues. These are local Linux checks, not hosted CI or a whole-repository,
full-host/browser or release run.

The fresh, independently reviewed fixture provisioner verified its existing
cluster/volume identity before creating only new scoped databases and roles.
Actual storage migrations applied the operational registry1 and foundation
registry7 to separate databases. This fixture does not qualify a complete generated
17-migration application. Distinct restricted TLS roles and actual pg_dump16.15
exercise dump/publication, restart, independent writer-pool admission, lease expiry,
fencing, retention/capacity, clock regression and blocked-dump cancellation.
Existing fixtures, LAN preview, databases and private configuration were preserved.

An actual TLS metadata/STS/S3-shaped SDK fixture verifies the same SigV4 credential
snapshot, decoded archive bytes and typed manifest/hash/acks around a real
restricted PostgreSQL dump. The next real UTC slot's partial upload preserves the
prior successful snapshot. Actual compiled CLI status/check uses only the restricted
reader, with unused credentials poisoned and ledger state unchanged. Concrete
ScheduleReader → operations observation → alert evaluator retains stale snapshot
age after failure and reports a firing backup-freshness condition.

Two compiled negative mutations produced the predicted RED results: manufactured
success after a failed upload, and replacement of the captured credential snapshot
with a refreshing source. Exact restoration passed targeted race checks. Final
production SHA256 values are:

- `internal/backup/schedule_run.go`:
  `4127d3fe38c7c5c42e77550bf74573ca237af9eb698f71b7b196f387c4d69bd7`
- `internal/backup/runtime_store.go`:
  `8e27a22acab2ace5185468aac350d7110ba51af2a7b084c7f85024d1f823b820`

Review additionally required independent schedulers/database pools and explicit
rejection of delayed successful acknowledgements after successor admission and
completion. Both final changed race tests passed, zero skips, with vet/lint clean.
This test-only delta plus adopted design documents yields independently approved
source head `52c6640f952519aba5a2da2b811232f12cf1e641`; production code is byte-for-byte
unchanged from the 210-check candidate.

Offline VM assets and the public Go/Pulumi component are declarations. Tests check
actual resource graph/argv/environment-file references, disabled state, exact
invocation permissions, account mismatch, and matching policy dependencies.
Systemd settings retain selected file visibility and give the reader no staging
write allowance. No actual systemd namespace/installation is qualified.

Actual managed CLI render produced a disabled 6,744-byte bundle. A syscall trace
observed zero IPv4/IPv6 network calls. Its generated public `infra/backupschedule`
recipe compiled in a standalone external Go module against the exact candidate,
using unchanged pinned dependencies with package/sum downloads disabled. Render
never installed or enabled resources. Actual image/entrypoint/config bytes,
secret-file separation, effective IAM/trust, private routes and trigger delivery
remain owner/provider qualification gates.

## Review and public-artifact limits

Independent review found and resolved empty-directory EOF handling, descriptor-
relative retained-file statistics, permission-resource ordering, generated service
path visibility and the two final boundary proof gaps. Design PR137 adopted v1.34
before initial source dispatch. Separate reviewed PR138 adopted v1.35 and landed
`728f3092d88fdfa1bb6329bcf0aa968519c373ae` before final source integration; its entire
tree equals reviewed `816752783a9b4777a3a24c2c2155d85d285614ee`, with fresh bounded
plan/projection/document scan checks. Scheduler trust uses the exact selected
group ARN and source account, consistent with the service-specific contract.

The unchanged artifact scanner remains red: baseline61 findings, candidate62.
Follow-up135 retains baseline reconciliation. Three changed-path findings have
independently reviewed narrow exceptions: two moved pre-existing empty-URL/private
variable handling lines in create.go and one new in-memory dump URL variable
passed into Create in schedule_run.go. None contains a literal credential/private locator.
No scanner suppression or weakening was added; do not call this scan green.
Bounded plan semantics retain all1,598 tasks; in-memory projection checks do not
qualify native admission, receipt authenticity or release.

## Delivery coverage

Requested batch ceiling10; this coherent member is T10.9 only, with accepted
T10.8/T10.4 dependencies. Foreign-owned and provider/source-blocked tasks were not
claimed or padded into this delivery. The integrator owns shared seams; two
inherited-session authors own disjoint core and runtime/CLI paths and a separate
reviewer supplies independent assessment. No model/effort overrides were supplied;
runtime token/cost totals are unavailable, so no acceleration/cost claim is made.
Original task authority remains held pending reviewed landing and task acceptance.
