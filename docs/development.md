# Go development

AMOS targets Go 1.27.x as its current supported toolchain line. Go 1.26.x is
the prior supported line while upstream security fixes remain available. The
module sets a Go 1.27 language/toolchain floor and pins the current patch
toolchain for reproducible local work; update that pin when adopting an
upstream patch release. Confirm current support in the [official Go release
history](https://go.dev/doc/devel/release) and install builds from
[go.dev/dl](https://go.dev/dl/).

The foundation uses `github.com/jackc/pgx/v5` v5.11.0 through `database/sql`
for PostgreSQL. pgx is the maintained stable v5 driver/toolkit and is licensed
under MIT. The direct dependency is pinned in `go.mod`; `go.sum` records its
module checksums.

Unit checks need Go 1.27.x:

```sh
go version
make test
make vet
```

Real database integration checks require a disposable PostgreSQL database and
a URL in `AMOS_TEST_DATABASE_URL`. The database user needs permission to
create and drop schemas. `internal/testkit` creates a cryptographically random
owned schema for each test and drops only that schema during cleanup.

```sh
psql "$AMOS_TEST_DATABASE_URL" -c 'SELECT version()'
make test-integration
```

Integration mode fails when the URL is missing or PostgreSQL is unreachable;
it never silently skips or substitutes a fixture. Use a disposable database,
because test cleanup drops its generated `amos_test_…` schemas with `CASCADE`.
