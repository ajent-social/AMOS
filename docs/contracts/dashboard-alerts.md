# Generated dashboards and finite alert definitions

Amendmentv1.30 freezes T10.6 after bounded metrics. Independent design landing
precedes source. This creates usable local generation/evaluation, not a deployed
monitor, scraper, notification system or operational promise.

## Inputs and generated artifact

The generator requires an explicitly selected, valid `opsconfig.Config`; Defaults
alone are not a selected installation target. Activation stays explicit. Additional
owner-supplied values are required: maximum HTTP5xx percentage (0–100 exclusive),
mean HTTP latency in milliseconds, database in-use percentage (0–100 inclusive
upper bound), queued depth, queued age, certificate-expiry warning lead time,
sample freshness, failure hold and recovery hold. Durations are finite, nonnegative
where zero means immediate hold, and at most7days; positive thresholds cannot be
zero. Certificate lead may be at most90days. Do not invent/activate these values
for a real owner. Synthetic fixtures use explicitly selected fictional roles.
Use the selected RPO and disk threshold from the validated operations targets.

Produce deterministic closed JSON `amos-dashboard-alerts-v1`, with seven fixed
panel/rule IDs: `service_errors`, `service_latency`, `disk_pressure`,
`database_pool`, `queue_backlog`, `backup_freshness`, `certificate_expiry`.
Each rule names its finite measurement/comparison, explicit threshold/duration,
severity (`warning` or `critical`), bounded owner-role label, local runbook link
and recovery condition/hold. Generate the referenced actionable runbook text in
the same bundle. Rule/metric names cannot be caller-created expression strings.
No arbitrary queries, templates, URLs, raw logs or label maps are interpreted.
Typed JSON marshaling handles configuration values; no unsafe text substitution.

The artifact is at most64KiB, includes its version and finite numeric parameters,
and always declares notification sending disabled. A destination may be absent
or an owner-supplied bounded credential reference for future configuration; no
credential values or actual destination authorization are inferred. Neither an
activation flag nor an artifact field grants send authority. This implementation
has no send code, destination resolution, network I/O or automatically enabled
external actions. A future authorized notification integration is separate.
Runbook paths are fixed bundle-local references; never fetch them as URLs.

## Measurements and evaluation

Evaluate the generated artifact itself, using fixed typed measurements; do not
validate one rule representation and execute unrelated handwritten thresholds.
A library adapter may derive HTTP status/duration counters, pool pressure and
queue gauges from actual `amos-metrics-v1` snapshots. It must not relabel generic
worker last-success as a successfully published backup. Disk usage, successful
backup completion and certificate expiry are separate explicit supplemental
inputs. Missing or stale supplemental data is unavailable, not healthy zero.
This task does not implement those host/provider collectors.

Every observation has a trusted-local observation time, explicit available flag
and bounded numeric fields or UTC timestamps. This is evaluator input, not proof
of source authenticity or a remote collection contract. Reject nonfinite values,
future observations, backward/out-of-order evaluation time, invalid ranges and
excessive sample/count/byte limits. No owner, provider, URL, principal or business
identifier becomes a measurement label. Samples never grant notification authority.

Service error percentage is the delta HTTP5xx count over delta completed2xx–5xx
requests for two fresh snapshots. Mean latency is delta duration sum/count,
explicitly labeled mean, not percentile/SLO evidence. Counter decreases/resets,
invalid deltas and zero-traffic windows are unavailable; they cannot clear an
active incident. Pool pressure requires a positive configured max-open count.
Queue rule fires on either configured queued depth or queued age. Unknown/leased
backlog remains visible, but does not pretend to be a queued age measurement.
Backup age is evaluation time minus a successfully completed backup timestamp;
failed attempts/dump creation/worker success cannot advance it. An unknown last
successful backup is unavailable. Certificate lead is expiry minus evaluation
time; an already expired certificate is an adverse condition. Disk percent and
backup age have distinct rule IDs and independent incident state.

Rules move through `disabled`, `unavailable`, `healthy`, `pending`, `firing` and
`recovering`, emitting `recovered` only after fresh recovery criteria hold for
the configured recovery duration. Faults must hold for the declared failure
hold; intermittent/stale input cannot accumulate that time. Unavailable input
preserves an existing firing incident as unresolved and resets pending holds;
it never silently becomes recovered. Fresh recovery starts its own hold.
Unactivated targets remain disabled. Results include only fixed IDs/states and
safe numeric observations; they contain no raw errors, input files or destinations.

## Executable local path

Add `amos ops render --config <file>`: read a bounded strict local JSON config,
validate targets/options, and write only the deterministic generated bundle to
stdout. Add `amos ops evaluate --definition <file> --samples <file>`: strictly
validate the generated bundle and a finite chronological sample sequence, execute
its rules, and write fixed-state results. No persistent watcher, network call,
provider lookup, filesystem overwrite or notification occurs. Bad input produces
a generic nonzero failure without echoing owner/credential/input contents.
Definitions/configs reject duplicate decoded keys, unknown/case-alias fields and
unsupported versions. Maximum256 samples and1MiB sample input; enforce finite
nested structures before typed JSON decoding can ignore aliases/extra elements.
The CLI is a concrete generator/evaluator, not a claim of deployed monitoring.

## Delegation and acceptance

T10.6 owns `internal/scaffold/ops/dashboards.go`, tests and finite alert templates.
The integrator explicitly delegates directly necessary new `cmd/amos/ops.go` and
tests plus minimal main dispatch. No shared migration, root dependency, unrelated
CLI behavior, cloud resource or default application-listener change. The existing
initializer/runtime generator must remain usable.

Exercise actual generator output and CLI render/evaluate with healthy, degraded,
pending, firing, missing/stale/reset and recovered sequences. Assert all seven
panels have valid finite rules, severity, owner/runbook/recovery, no notification
sends can be enabled, and unset/invalid owner options fail. Advance successful
backup age past selected RPO and hold: it fires independently of disk pressure;
fresh successful backup plus recovery hold resolves it. A compiled mutation
reversing/disabling the RPO comparison must fail the negative regression, then
exact restoration must pass. Preserve actual source/race/vet/lint and public-safe
checks, independent final review, guarded merge and fresh landed CLI verification.
Synthetic measurements do not qualify live providers, real collection, delivery,
SLO/RPO achievement, production deployment or E16 release.
