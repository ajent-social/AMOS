# ADR048: durable backup publication scheduling and truthful freshness

Status: adopted. Initial v1.34 design landed independently in PR137 before source
dispatch; this v1.35 compatibility correction is adopted separately before source
integration. This is design adoption, not source or provider acceptance.

Adopt [backup scheduling v1.35](../contracts/backup-scheduling.md) for T10.9.
Compose the actual pinned PostgreSQL dump and existing manifest-last backup Store
with a dedicated PutObject-only AWS adapter, PostgreSQL schedule/attempt ledger,
executable one-shot commands and profile-specific invocation assets. Distinct scheduler
writer and dump reader identities use separate explicitly TLS-verified operational
and application databases; a third read-only ledger identity serves status. The
operational database has its own opt-in scheduling migration registry, preserving
application/generated migration prefixes. No ambient PostgreSQL credentials or
default routing. Immutable
config/destination digests reject rebinds. Indexed status reads and an explicit
retained-attempt cap bound ledger access; reaching the cap denies admission and
requires separately approved maintenance, not automatic history/object deletion.
Explicit local artifact/byte bounds similarly fail closed; retained staging files
are not automatically recursively removed. Application
request traffic and generic worker success are not backup scheduling or freshness.

A fresh immutable UUIDv7 identifies every admitted attempt and its isolated remote
keys. Reserve UTC slots and current fenced authorization transactionally using
fresh database time. Coalesce missed slots without backlog bursts; one attempt
spends a slot even if it fails or crashes. Later slots always allocate fresh IDs.
Only a successful Create→archive+manifest acknowledgement→current fenced database
completion advances publication freshness. Local manifests, orphan archives,
duplicates, uncertain responses and stale completions cannot manufacture success.
A publication followed by a ledger-write crash under-reports success safely.

PostgreSQL exclusion cannot fence an S3 request already in flight after partition,
lease loss or clock change. Promise one **currently authorized** attempt, bounded
cancellation and no stale ledger completion, not physical exactly-once execution,
zero physical overlap or atomicity across the two systems. Do not add read/delete
or recovery credentials to repair unknown writes. Retained orphans remain subject
to separately selected storage retention and recovery procedures.

Runtime AWS credentials use only explicitly selected IMDSv2 or ECS task metadata
routes, owned bounded transports and a regional STS account/assumed-role check.
No deployment loader, default chain, static/profile/OIDC fallback or caller boolean
establishes runtime authority. STS and S3 use one captured expiring credential set
whose remaining life covers the attempt/cleanup budget, with no hidden refresh.
Core construction receives the resolved dump URL explicitly; only CLI composition
resolves the command's selected environment references. Use existing pinned SDK packages. Generated source
and local metadata/STS/S3-shaped fixtures are not proof of real AWS identity,
effective IAM/KMS policy, encryption, versioning, retention or trigger delivery.

A separate reader uses dedicated read-only ledger credentials and no Store,
metadata/STS client or writer/dump secret resolution. Shared nonsecret target and
config identity bind its observations without promoting credential authority.
Make the existing upload hash/read loop context-aware; measure cancellation on
protected healthy local regular files, not an allegedly interruptible arbitrary
kernel/network filesystem operation.

Read-only exact-stream status preserves attempted/completed/success timestamps
separately. Database query/clock uncertainty is unavailable. Failed uploads leave
previous success aging. Feed the existing finite backup alert through a derived
supplemental observation whose value is the original snapshot timestamp of a
successfully published backup; generic jobs LastSuccess remains unrelated. The
publication acknowledgement time stays separate and never resets the data-age
clock. Narrow the existing dashboard wording without renaming its measurement.
A recent upload is not an RPO guarantee. One-shot checks and generated assets do not create a persistent
incident monitor or enable notification sends.

Render disabled-by-default systemd VM and Go/Pulumi managed scheduling assets from
an explicit separate strict assets document, using the same real commands and
database slot gate. Freeze paths/OS identities, immutable task/image/config refs,
entrypoint/argv, environment-file mappings, network/role refs and exact pass-role
scopes. Offline rendering marks provider mappings unverified; it never claims it
inspected task definitions, file bytes, private routes or effective permissions. Do not
install/activate resources or choose real accounts, roles, regions, destinations,
credentials or retention policy. Root expands the original leaf-task ownership for
CLI/adapter/schema/grants/profile wiring before source dispatch. Required actual
SQL/dump/TLS/CLI and failed-upload freshness mutation proof precede source
acceptance; independent review/landing and provider/release gates remain separate.
