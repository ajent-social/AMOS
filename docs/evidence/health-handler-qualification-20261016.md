# T10.2 bounded health-handler qualification

Source: `4029237931315345492d73785b789e4d43151132`. No application source
is changed by this acceptance update. Previously returned, independently reviewed
and landed handler/host wiring is requalified against T10.2's original criteria.
This is integrator-owned acceptance review, not takeover of the retained author
claim or native lifecycle admission.

## Requirement-to-evidence audit

| Task requirement | Current source and obtained verification |
| --- | --- |
| Liveness independent of transient dependencies | Handler liveness does not call readiness participants; actual disconnected-pool test keeps liveness 200 while readiness is 503. |
| Bounded DB and migration compatibility | Required ping and callback share one deadline; timeout/parent cancellation tests pass. Actual NewLocal uses its SQL pool/schema callback; dropping its tested schema constraint while DB remains reachable gives readiness 503/liveness 200. |
| Required versus optional dependencies | Constructor rejects missing required checks; optional errors/deadlines report degradation without turning healthy required participants into unavailable. |
| Actual HTTP status/body/cache/error privacy | Handler, public App, runtime delegation and real NewLocal tests cover fixed JSON, status, no-store, reserved authority and omission of raw diagnostics. |
| Disconnected DB negative and restoration | Real PostgreSQL fixture pool closes: readiness 200/503, liveness 200; a newly opened validated pool restores readiness 200. The database container is not stopped by this test. |
| Regression detects incorrect readiness | Compiled isolated mutation returns 200 for the DB-error branch. `TestHTTPPostgresDisconnectAndRestore` fails with the predicted disconnected readiness 200/want503 assertion. Original bytes restored; real test passes again. |

## Executed checks

Go 1.27.1, Linux ARM64, isolated PostgreSQL 16.15. The required real-database flag
was explicitly enabled. Four-package scoped race command passed 19 named
tests/subtests, zero skips. Package elapsed seconds: app 1.026, health 1.061,
runtime 1.021, apphost 3.188. Health vet and full health lint passed (zero issues).
Source restoration was checked against Git after the negative test. Public-safe
artifact and current-source/projection checks accompany the review candidate.

```sh
AMOS_HEALTH_POSTGRES_REQUIRED=1 go test -race \
  ./internal/health ./apphost ./app ./internal/runtime \
  -run '^(TestHTTP|TestReadinessTimeoutIsUnavailable|TestOptionalReadinessTimeoutDegrades|TestParentRequestCancellationDoesNotBecomeOptionalDegradation|TestNewHandlerRejectsMissingRequiredChecks|TestHTTPPostgresDisconnectAndRestore|TestLocalHealthUsesDatabaseAndSchemaReadiness|TestHealthHandlerForwardingAndAuthority|TestDelegatedHealthPreservesReservedSurface|TestHealthHandlerRejectsCompetingReadinessAuthority)$' \
  -count=1 -json
go vet ./internal/health
golangci-lint run ./internal/health
```

Operator supplies AMOS_TEST_DATABASE_URL for an isolated actual PostgreSQL
fixture and owner-local caches. No cloud credentials, provider mutation or
external spending is involved.

## Scope of acceptance

This qualifies the minimal T10.2 handler and its existing local composition.
NewLocal's compatibility callback checks selected session/factor schema and its
operation constraint. It does not prove the entire migration ledger, every
product table, production TLS/runtime roles, deployed supervision or recovery.
Those are separate contracts/E16 gates; they are not silently represented by a
200 readiness response or waived by task acceptance.

T10.3 remains in progress: current request logging/privacy components are
present, but its public app/business-service correlation acceptance needs an
exact boundary and executed evidence. It is not accepted from this health audit.

## Delivery unit

Requested batch ceiling 10; actual member 1 (T10.2 requalification). One current
session verifies immutable source; independent reviewer evaluates this audit,
original acceptance and final diff before guarded merge. No author-source claim
is released or changed. Inherited session selection is preserved; exact model,
token/cost telemetry and review duration are unavailable. No acceleration claim.
Independent exact-head review approved `9621a0a3271405443fbc4a4618a1de3085be0250`
against source `4029237931315345492d73785b789e4d43151132`; the reviewer independently
reran the required-PostgreSQL full health race suite with zero skips. PR #119
rebased to `8d00b54b326efa234c4b3aeb6627c797538f0848`; the entire landed tree
matches the reviewed candidate and is reachable from main. A fresh landed
required-PostgreSQL full health race run passed, zero skips (1.044 seconds).
No hosted CI checks were configured. The supplemental qualification claim was
released; the retained original author claim was not altered.
