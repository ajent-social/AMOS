# T10.4 bounded metrics implementation evidence

Design: PR #122 landed `65ceb3e31491b5563f75643d8b0cf52d69bf69bb` after
independent exact-head approval. Application source candidate was implemented
against that design; the integrator rebased it onto docs/schema-only T14.1 main
`c41a0669cd07ad8f5228caa4bb02fa5d6a83f608`. No metrics source/test bytes changed
by that rebase. Root manifests and migrations remain unchanged.

## Obtained behavior

- Actual App/runtime success, error and panic observations use finite ordinal
  routes, sanitized status/error vocabularies, saturating counts and histograms.
  Trusted outer logging retains one ID/log and its panic control; the same
  collector observes exactly once, while an outer logger cannot suppress metrics.
- Copied finite configuration and snapshot slices; hostile/unmatched paths,
  status/error inputs and private canaries cannot create labels or leak to JSON.
- Separate actual-peer loopback handler validates method/port/address, GET/HEAD,
  no-store and maximum configured output size; no public App route/listener added.
- Actual PostgreSQL retry-to-dead, success and unknown backlog, retained retry
  gauge reduction, DB-clock last success and canceled/unavailable snapshots pass.
  Scope/kind exclusion is tested independently of legacy unscoped behavior.
  Read-only snapshots leave exact row/fence content unchanged.
- Actual pool saturation/waits/closed handles and existing NewLocal worker cycle
  pass. Unknown/stale/unavailable metrics never substitute healthy empty state.
  Snapshot errors preserve existing job results and consumer panic identity.

## Executed commands and negative check

Linux ARM64, Go1.27.1, isolated actual PostgreSQL16.15 and owner-local caches:

```sh
go test -race ./internal/telemetry ./observability ./jobs ./jobs/sqlstore ./storage ./app ./apphost ./internal/runtime -run TestMetrics -count=1 -json
go test -race ./internal/telemetry ./observability ./jobs ./app ./internal/runtime -count=1 -json
go vet ./internal/telemetry ./observability ./jobs ./jobs/sqlstore ./storage ./app ./apphost ./internal/runtime
golangci-lint run ./internal/telemetry ./observability ./jobs ./jobs/sqlstore ./storage ./app ./apphost ./internal/runtime
```

Focused checks:30 named tests/subtests, zero skips. Full five-package suites:
104 named checks, zero skips. All eight affected packages passed vet and lint
reported zero issues. A broader six-package attempt obtained124 passes but failed
`TestJobStoreRuntimeRequiredService` because AMOS_JOBS_RUNTIME_TEST_CONFIG was
absent. This full SQLstore suite is explicitly not claimed green. Other required
TLS storage/host and browser qualifications remain separate; no skip or
prerequisite policy was changed. Operator supplies AMOS_TEST_DATABASE_URL only
for the disposable actual database; no provider credentials are needed here.

A compiled mutation allocated HTTP series for raw unrecognized path/template
labels. The regression failed with8,003 series/want3. Original bytes restored;
the same cardinality/privacy regression passed. Mac committed source/test tree
matched the executed DGX staged tree before docs-only rebase. Diff/format and
current-source/projection checks accompany final review.

## Scope and delivery

This qualifies the original bounded application/worker metrics behavior and
local composition, not live providers, TLS runtime-role admission, private-network
operator profiles, deployed supervision, alerting, exporter or E16 acceptance.
The independently versioned diagnostic source baseline does not automatically
admit these modified observability files for external reporting.

Requested batch ceiling10; this coherent source delivery has one product member
(T10.4), parallel to disjoint diagnostic work. One retained source implementer
preserves inherited session selection; final independent review, guarded merge
and fresh landed verification remain required. Exact token/cost telemetry is
unavailable; no acceleration or hosted CI claim.
