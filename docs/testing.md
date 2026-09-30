# Test harnesses

The current tree has no composed AMOS application routes or product UI. These
test suites provide real HTTP/browser boundaries through test-only fixtures;
they do not claim that product endpoints or provider adapters exist.

## API and database

`tests/harness` uses `net/http/httptest` to send requests through an actual
HTTP server and checks both status and body, including an unauthenticated
request that must receive 401. Its integration-tagged case uses the shared
PostgreSQL testkit to create an isolated schema, checks that a denied write
does not persist, and verifies a permitted test-fixture request in PostgreSQL.
The fixture route and bearer value exist only in `_test.go` files and do not
provide a production auth or provider implementation.

```sh
go test ./tests/harness
./scripts/test-api.sh unit
```

The integration suite requires a disposable PostgreSQL database in
`AMOS_TEST_DATABASE_URL`. The user must be able to create and drop schemas.
It fails explicitly if the URL/service is missing.

```sh
psql "$AMOS_TEST_DATABASE_URL" -c 'SELECT version()'
./scripts/test-api.sh integration
```

`./scripts/test-api.sh providers` is a separate opt-in gate. It first requires
`AMOS_RUN_PROVIDER_TESTS=1`, then fails with an explicit unavailable-suite
message until real provider adapters and provider-qualified tests are added.
Provider fixtures never satisfy this gate.

## Browser

The isolated harness pins `@playwright/test` to 1.63.0. Install its Node
dependency without downloading browser binaries:

```sh
npm ci --prefix tests/browser
```

Playwright's bundled Chromium is the default. Install it with
`tests/browser/node_modules/.bin/playwright install chromium`, or point
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` at a locally managed Chromium/Chrome
binary. The override is an environment setting; no machine-specific browser
path is part of the repository. The fixture server starts and stops with the
Playwright run and serves a server-rendered synthetic page on loopback.

```sh
./scripts/test-browser.sh fixtures
```

The wrapper lists the selected tests before execution and fails if the
selection is empty. `reference`, `initializer`, `generated-app`, `upgrade`,
and `personal-billing` are reserved suite names from task contracts; each
exits nonzero until its real product UI and tests exist. Unknown names also
fail instead of passing an empty suite.

See the [official Playwright installation guide](https://playwright.dev/docs/intro),
[browser configuration guide](https://playwright.dev/docs/browsers), and
[release notes](https://playwright.dev/docs/release-notes) when updating the
pinned version.
