# ADR 026: Runtime-only PostgreSQL storage boundary

Status: proposed for acceptance after independent exact-head review and merge.
Date: 2026-10-06.

## Problem

The existing `storage.Open` creates both an application pool and a
migration-protocol pool, and its returned `*storage.DB` is accepted by
`storage.Migrate`. Development depends on that compatibility, while a production
application process should not hold migration authority or a migration
credential. A runtime opener also needs explicit TLS trust and must not inherit
connection choices from ambient PostgreSQL environment variables or files.

## Decision

Adopt the runtime-only PostgreSQL contract in [v1.15](../contracts/runtime-storage.md).
Keep `storage.Open`, `*storage.DB`, and `storage.Migrate` intact for existing
development and migration flows. Add an independent `*storage.RuntimeDB` with a
single pool, private state, `WithTx`, `PingContext`, and idempotent `Close`; it
does not embed or expose `*storage.DB` and is not assignable to `Migrate`.
Define the minimal `TxRunner` interface containing only `WithTx` so later
runtime service constructors can accept either handle without breaking local
development callers.

The strict runtime configuration binds one lowercase ASCII DNS A-label host,
explicit port/database/user/password, explicit PEM roots, and finite startup and
pool limits. The pgx parser bridge is limited to internally constructed
allow-listed input with all endpoint and credential fields explicit, no service
selection, nonempty password, parser TLS disabled, and `sslrootcert=` explicitly
empty. After parsing, code installs a fresh verified TLS configuration from the
copied PEM roots, clears fallbacks and runtime parameters, and validates every
connection field before creating the pool or making a network call. Startup
rejects the pinned pgx version's supported nonempty `PG*` variables without
mutating process environment; concurrent environment mutation is outside the
contract.

The callback error from `WithTx` propagates unchanged for compatibility; its
caller owns safe public error mapping. Callback panic triggers deferred rollback
and rethrows the same panic. Storage-generated errors remain sanitized.
Readiness performs bounded read-only checks and the runtime opener never
migrates or repairs schema. Caller cancellation propagates from startup ping;
the opener's own startup-timeout expiry and driver failures map to
`ErrUnavailable`. Close marks the handle closed before pool shutdown; already
admitted operations may finish or fail under their contexts, and an arbitrary
callback that ignores context is not preempted. Close does not promise bounded
return while such work is still running; closing a nil handle is a no-op.

## Consequences and ownership

The first implementation assignment owns only `storage/runtime.go` and
`storage/runtime_test.go`. It does not change existing storage or service APIs,
module files, job-pool integration, generated applications, or executable
composition. A later separately reviewed production constructor must select
`RuntimeDB` exclusively and receive no migration credential. The actual runtime
database role must deny DDL, migration-ledger mutation, ownership, and role
changes; the Go API distinction alone does not sandbox SQL.

Tests use an isolated TLS-enabled PostgreSQL service and a synthetic
least-privilege role. They do not qualify the selected production endpoint, role,
network, deployment, provider, or operational recovery. Production acceptance
requires those distinct gates.
