# Bounded application and worker metrics

Amendment v1.28, T10.4. This design adds optional numeric operational signals;
it does not qualify supervision, alert delivery, deployment or provider health.
Independent design landing precedes source dispatch.

## Collection and public composition

Expose a standard-library-only public `observability` facade for a concurrency-safe
collector implemented by `internal/telemetry/metrics.go`. `observability.New(Config)`
returns a collector or a generic configuration error. Config contains copied
`RouteTemplates []string` and `WorkerNames []string`: at most32 unique canonical
registered templates and16 unique bounded worker names. Empty sets are valid.
Templates use the runtime registration syntax and a maximum256 bytes; worker
names use1–64 ASCII letters/digits/dot/underscore/hyphen. Invalid, duplicate or
oversized input fails before serving. Config names never become metric labels.
The caller owns this finite operator configuration; it is never derived from
request paths, payloads or discovery traffic.

`app.Options.Metrics *observability.Collector` and optional
`apphost.LocalConfig.Metrics *observability.Collector` install observation around
the existing runtime dispatch. Nil preserves existing behavior. Registered
runtime templates select a configured ordinal `r00`–`r31`; unmatched paths use
`unmatched` and registered-but-unconfigured templates use `other`. Templates,
URLs, query values and private route names do not appear in output. No caller
ID, identity, workspace, provider account, header or payload becomes a label.
The logger retains its existing independent privacy and trusted-ID behavior.

Collectors accept only server-selected templates, sanitized status and elapsed
time. Expose counts by ordinal route and status class (`1xx` through `5xx`,
`other`), error counts by route and the existing finite safe-error-code set,
and duration histograms by route. Histogram upper bounds in milliseconds are
5,10,25,50,100,250,500,1000,2500,5000,10000 and infinity. Counts and sums saturate
instead of wrapping; negative durations clamp to zero. HTTP series are bounded
by these finite cross products even under arbitrary paths/statuses/errors.
No request observation performs network/database I/O or creates background work.

## Database and worker seams

The facade accepts a pushed `database/sql.DBStats` snapshot from trusted
composition, exporting only open/in-use/idle/max connections, wait count and
wait duration. No DSN, query, parameter or server identifier is retained. Add
safe local Stats accessors to the two storage handles; closed handles report
unavailable explicitly, rather than healthy zeroes. No raw pool handle escapes.

Add an optional observer to `jobs.Worker`, without changing Repository or
Consumer method contracts. Events contain only bounded execution outcome and
elapsed time, never Job, ID, lease owner, payload, consumer error or provider
response. Invalid, canceled, failed claim/resolve and retry/dead/unknown outcomes
remain distinct bounded events. Observer failures must not change job results;
collector observation is synchronous, in-memory and nonblocking on external I/O.

Add a read-only `jobs/sqlstore` aggregate snapshot using its existing transaction
runner and the exact configured kind/scope predicates. It returns queued depth,
oldest queued age, total retry attempts, dead count and last-success time. Use
database time, fixed states and numeric aggregates; no job record/material is
returned. Last success is explicitly unknown if absent; unavailable collection
must not be reported as empty/healthy. Retry total is a retained-row gauge,
not a lifetime promise: deletion/retention may reduce it. Queries are bounded by
caller cancellation/deadline and do not claim, repair or mutate jobs.

The collector binds worker observations/snapshots to the configured ordinal
`w00`–`w15`; unknown worker names are rejected without allocating a new series.
Local host composition obtains aggregate/database snapshots on its existing
worker cycle with a finite deadline. Export observed/stale/unavailable state
and observation time. Missing callbacks or collection errors are unavailable;
last-good values must be marked stale. No metrics failure may dispatch work,
claim provider availability, bypass job fencing or weaken existing transactions.

## Private operator output

`Collector.Handler()` returns a separate handler; it is never mounted on App's
public routes or generated public listener. Serve it only on a distinct
operator-owned loopback listener. The handler independently checks the actual
RemoteAddr is a loopback IP, accepts GET/HEAD only, and rejects remote,
malformed or absent peers before exposing data. Forwarded headers never grant
access. Production private-network/authenticated operator profiles need their
own qualified contract; do not treat reverse-proxy headers as loopback proof.

Use versioned JSON `amos-metrics-v1`, fixed closed fields, numeric aggregates,
ordinal labels and fixed enum states. No maps keyed by caller material. HEAD
matches GET headers/status without body; responses set application/json and
no-store. Cap output at256KiB and fail generically if that invariant is broken.
Rendering copies a bounded in-memory snapshot; it invokes no provider/query
callbacks. There is no automatic listener, exporter, external send, scrape agent
or retention service. The owner may omit the collector or stop the operator
listener. Counts are process-local, reset on restart; durable job gauges are
explicit snapshots, not claims of exactly-once operational telemetry.

## Source assignment and acceptance

After design review/landing, T10.4 owns telemetry metrics and tests. The integrator
explicitly owns/delegates only the directly necessary new observability facade,
App/runtime optional observation, storage Stats methods, jobs observer and scoped
read-only aggregate, local-host composition and focused tests. No migration,
root dependency, unrelated API, public route, provider mutation or startup secret
change. Preserve nil compatibility and existing business/auth/job behavior.

Prove actual App success/error/panic observations, thousands of unique paths and
unrecognized error/status inputs cannot expand series; config is copied; no
private canaries reach JSON; output size/method/peer/HEAD/cache invariants hold.
Run actual PostgreSQL worker retry/dead/queued/success cases and show failures
increase failure/backlog signals with correct foreign-scope exclusion and no
mutation by snapshot reads. Exercise host wiring and stale/unavailable states.
A disposable mutation using raw request paths as metric labels must fail the
cardinality/privacy regression; restore exact source and pass. Independent
source review, guarded merge and fresh landed checks precede task acceptance.
