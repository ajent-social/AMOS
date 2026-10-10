# Test harnesses

The generated application composes identity, workspace, health, and reserved
billing routes. Billing UI requires explicit server-owned catalog and provider
configuration; without it, the billing route fails closed as unavailable.
PostgreSQL-backed HTTP tests exercise the authenticated billing page and
workspace-scoped subscription projection. These checks do not qualify a live
provider or deployment.

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
go test ./apphost -run '^TestLocalBillingUsesRealSessionAndScopedApplicationCatalog$'
```

The billing integration test signs up and verifies a real session, renders
server-configured catalog choices, and checks that only a fresh confirmed
subscription projection reveals the configured action. Its provider is a test
stub; it does not qualify provider behavior.

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
and `personal-billing` remain reserved browser-suite names; each exits
nonzero until its dedicated browser journey exists. Billing currently has
PostgreSQL-backed HTTP session coverage, not a dedicated Playwright journey.
Unknown names also fail instead of passing an empty suite.

See the [official Playwright installation guide](https://playwright.dev/docs/intro),
[browser configuration guide](https://playwright.dev/docs/browsers), and
[release notes](https://playwright.dev/docs/release-notes) when updating the
pinned version.
