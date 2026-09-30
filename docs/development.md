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

## Required Go quality gates

Use `make fmt`, `make lint`, `make test`, and `make test-integration`. The lint tool is pinned to golangci-lint 2.13.2; the checked-in version-2 configuration enables error checks, vet, assignment checks, static analysis, and unused-code checks. Install that exact release from the [official distribution](https://golangci-lint.run/docs/welcome/install/); no command silently installs or substitutes a different tool.

Both whole-module test targets require `AMOS_TEST_DATABASE_URL` and execute durable tests against real PostgreSQL. The integration target also includes tagged HTTP/schema checks. Missing required services fail; product/provider/browser suite selectors that are not implemented fail separately. `make test-api` executes the real HTTP fixture suite, and `make test-browser` executes the pinned Playwright fixture suite after `npm ci --prefix tests/browser`. Fixtures are harness evidence, not a product release.

On a shared build host, check current load and acquire the configured shared build lease before whole-module tests, vet, or lint; release it immediately after the command. Parallel scoped package tests are bounded by the host policy. The script does not assume that every user's computer has a particular host or lease repository.
