# Runtime-only PostgreSQL storage contract (v1.15)

Status: proposed adoption with amendment v1.15; freeze for implementation follows independent exact-head review and merge. This design does not qualify a production database, role, TLS endpoint, deployment, or runtime.

## Purpose and compatibility

The application runtime needs a PostgreSQL connection boundary that cannot be
passed to the migration API. Preserve the development `storage.Open`,
`*storage.DB`, and `storage.Migrate` APIs and their existing behavior. Add the
separate `*storage.RuntimeDB` described here. It has one application pool and no
migration pool, migration method, raw-pool accessor, or configuration accessor;
it does not embed `*storage.DB` and cannot be passed to the existing
`storage.Migrate(context.Context, *storage.DB, migrations.Registry)` function.

This Go API boundary is not a SQL sandbox. The runtime database role must be
granted only the required runtime DML and read privileges and must be denied
schema, migration-ledger, ownership, role, and other administrative changes.
Production process composition must receive only the runtime credential; a
migrator uses its separate credential in a separate authorized process.

The initial source slice adds only `storage/runtime.go` and
`storage/runtime_test.go`. It does not alter existing services, `storage.DB`,
`Open`, `Migrate`, module files, job storage, executable composition, or
generated applications. `apphost.NewLocal` and its callback API remain
development-only and unchanged. Later assignments may adapt service
constructors through `TxRunner` and add a separately typed production
composition path.

## API and configuration

The exported shape is:

```go
type TxRunner interface {
    WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type RuntimeConfig struct {
    Host             string
    Port             uint16
    Database         string
    User             string
    Password         string
    RootCAPEM        []byte
    StartupTimeout   time.Duration
    MaxOpenConns     int
    MaxIdleConns     int
    ConnMaxLifetime  time.Duration
    ConnMaxIdleTime  time.Duration
}

func OpenRuntime(context.Context, RuntimeConfig) (*RuntimeDB, error)
func (*RuntimeDB) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
func (*RuntimeDB) PingContext(context.Context) error
func (*RuntimeDB) Close() error
```

`RuntimeDB` fields remain private. Both `*storage.DB` and `*storage.RuntimeDB`
satisfy `TxRunner`; the interface contains only `WithTx`. The runtime handle
also provides `PingContext` and idempotent `Close` but no migration capability.

`OpenRuntime` rejects a nil context and invalid configuration. `Host` is one
canonical lowercase ASCII DNS name using valid A-labels; uppercase is rejected
rather than silently folded. It may not be an IP literal, socket path, host
list, empty value, trailing-dot name, whitespace, or control-containing name.
`Port` is nonzero. `Database` and `User` are valid nonempty UTF-8 names of at
most 63 bytes without control characters. `Password` is nonempty and at most
4096 bytes. It is retained only in the private driver/pool configuration and
never appears in diagnostics.

`RootCAPEM` is nonempty and at most 1 MiB. It must contain one or more PEM
`CERTIFICATE` blocks, each containing a valid certificate, and no other block
types, malformed data, or non-whitespace trailing bytes. The implementation
copies the caller's bytes. It does not read a root file or use the system trust
store. Pool bounds are explicit: startup timeout is positive and at most 30
seconds; maximum open connections is 1 through 64; maximum idle connections is
zero through the open limit; connection lifetime is positive and at most 24
hours; connection idle lifetime is positive and at most one hour.

## Connection construction and ambient configuration

The implementation pins pgx v5.11.0 behavior for this source slice. Before
parsing, it rejects every nonempty supported PostgreSQL configuration variable:
`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGPASSFILE`,
`PGAPPNAME`, `PGCONNECT_TIMEOUT`, `PGSSLMODE`, `PGSSLKEY`, `PGSSLCERT`,
`PGSSLSNI`, `PGSSLROOTCERT`, `PGSSLPASSWORD`, `PGSSLNEGOTIATION`,
`PGTARGETSESSIONATTRS`, `PGSERVICE`, `PGSERVICEFILE`, `PGTZ`, `PGOPTIONS`,
`PGMINPROTOCOLVERSION`, `PGMAXPROTOCOLVERSION`, `PGCHANNELBINDING`, and
`PGREQUIREAUTH`. Startup is serialized and the process environment is not
mutated; the guarantee does not defend against application code concurrently
calling `os.Setenv` during startup.

Only an internally constructed connection string with an exact key allow-list
is passed to the pinned parser. It supplies explicit host, port, database, user,
and nonempty password, does not select a service, sets temporary
`sslmode=disable`, and explicitly sets `sslrootcert=` to shadow the pgx
home-derived CA default. The empty root path is required because pgx v5.11.0
loads a selected CA file before it handles `sslmode=disable`. The explicit
password prevents passfile lookup; the absent service setting prevents service
file lookup. The source and tests must establish that this parser path reads no
service, passfile, or TLS file contents. Pgx's unconditional metadata `stat`
checks (including home-derived certificate paths and built-in socket candidate
directories) are permitted; their results must not influence the connection.

Before constructing a pool or making a network call, the implementation
replaces parser TLS settings with a fresh `tls.Config` using copied trust roots,
`ServerName` equal to the validated host, `MinVersion` TLS 1.2, and normal Go
certificate and hostname verification. `InsecureSkipVerify` remains false. It
clears fallback configurations and runtime parameters, and verifies the final
host, port, database, user, password, TLS, and pool fields against the typed
input. No connection begins until those checks pass. No raw DSN, connection
string, password, endpoint, database, driver diagnostic, or certificate detail
may appear in storage-generated errors or logs.

The startup ping uses the smaller of the caller's deadline and
`StartupTimeout`. A caller context cancellation or deadline error is returned
as the context error; expiry of the opener's own startup timeout and other
driver/connectivity failures map to the safe `ErrUnavailable` sentinel.
Configuration failures map to `ErrInvalidConfig`. No listener, schema repair,
or migration is performed by the opener.

## Transactions, errors, and readiness

`WithTx` starts a context-bound transaction and commits only when the callback
returns nil; callback failure rolls back. If the callback panics, rollback is
attempted and the original panic is rethrown unchanged. Begin and commit failures
return the caller's context cancellation/deadline error when present; other
storage-generated failures use the safe transaction sentinel and never wrap
driver diagnostics. When rollback succeeds or reports `sql.ErrTxDone`, a callback
error is returned unchanged. If rollback otherwise fails on a non-panic path,
return `errors.Join(callbackErr, ErrTransaction)` (or join the existing safe
operation/context error with `ErrTransaction`), never the raw rollback error.
This matches existing `storage.DB.WithTx`; callback owners remain responsible for
public error mapping. On a panic path, always rethrow the exact original panic
even if rollback fails; the rollback failure is suppressed and must not replace
that panic. Nil, closed, or invalid handles fail visibly.

`Close` is idempotent and linearizes by marking the handle closed before closing
the single pool. Operations admitted before that point may finish or fail under
their caller contexts; later operation admissions return `ErrClosed`. An
arbitrary trusted callback is not preempted if it ignores its context, and
`Close` makes no bounded-return-time promise while previously admitted work is
still running. Closing a nil handle is a safe no-op; nil operation handles fail
closed.
`PingContext` returns caller context cancellation/deadline errors unchanged and
maps driver failures to `ErrUnavailable`.

Readiness remains outside `OpenRuntime`; its health check performs only the
documented read-only schema assertions. Runtime code never calls `Migrate`.

## Required evidence and limits

The source slice requires parser/configuration tests for every supported PG
variable, non-discovery of service/passfile/TLS file contents, strict PEM parsing,
copied trust input, finalized endpoint/TLS/pool fields, bounded startup and pool
configuration, callback rollback and panic restoration, callback-error
propagation, begin/commit context cancellation, deterministic rollback-failure
and panic-plus-rollback-failure cases, nil and closed handles, sanitized errors, and
deterministic `WithTx`/`PingContext` versus `Close` plus post-close/idempotent
close races. A real isolated TLS-enabled PostgreSQL service with a least-privilege
runtime role must prove positive runtime DML, hostname and root-CA rejection,
rollback, and denied schema, migration-ledger, and role operations. A
compile-negative check proves `RuntimeDB` cannot be passed to
`storage.Migrate`.

These local tests qualify only the local opener and synthetic role. Actual
production TLS, role grants, network access, secret isolation, deployed process
composition, production behavior, and operational recovery remain separate
gates. Neither this contract nor its tests authorize migration, provider, release,
or deployment actions.
