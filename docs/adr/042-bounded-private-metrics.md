# ADR042: keep operational metrics finite and private

Status: proposed; independent design review and landing precede source.

T10.4 becomes ready after request-logging acceptance. Adopt the optional
[v1.28 metrics contract](../contracts/bounded-metrics.md): ordinal route/worker
labels from copied finite configuration, fixed numeric aggregates, explicit
unknown/stale/unavailable values, and a separate loopback-only operator handler.
No public metric route or automatic exporter is introduced. JSON keeps the
first implementation independent of external telemetry services/dependencies.

Actual worker and database seams provide operational observations without
exposing jobs, credentials, identifiers or raw errors. Read-only aggregate
queries preserve existing worker scope/fencing. This design requires actual
PostgreSQL/runtime cardinality and failure checks before source acceptance;
it does not close tracing, alerting, deployment or E16 qualification.
