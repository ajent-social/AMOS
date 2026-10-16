# T10.3 public app and business-service logging qualification

Application source: `ff97e730167a3fe03091f8bb441b1fbf8f7625a8` (telemetry and
App code unchanged from `8d00b54b326efa234c4b3aeb6627c797538f0848`, the initial
executed base). The supplemental external-consumer proof imports only public
`app` from a separate Go module. Its actual App HTTP listener calls a distinct
loopback HTTP backend. No application API, logging implementation or trust rule
is changed by this qualification.

## Requirement and trust audit

The frozen planning contract rejects caller correlation as a trusted internal
ID. This takes precedence over T10.3's earlier provisional implementation text
about accepting bounded external correlation. A standard business handler reads
the generated response `X-Request-ID` installed before dispatch, validates it,
and forwards that diagnostic value on a fresh outbound backend request. It does
not forward the incoming ID, headers, query or body. The ID confers no identity,
workspace or other authority. This matches the operations and business-proxy
contracts without adding a public context API or caller-trust switch.

Five synthetic requests exercise success and failed backend responses, plausible
attacker-selected IDs, malformed IDs, duplicates and missing IDs. Six named
tests/subtests pass with zero skips. Assertions prove:

- Distinct generated 128-bit IDs correlate the handler, response, App JSON log
  and separate service JSON log; caller-selected values are never reused.
- Route template, method, status, duration and sanitized failure code remain
  useful. Actual caller paths are not logged.
- Injected bearer/cookie/provider/identity values, bodies, sensitive queries,
  caller IDs and backend diagnostic payloads appear in neither encoded log.
- The separate backend receives no caller credentials, identity headers or
  request target; its request body is a fresh synthetic safe-work value.

The existing internal telemetry tests separately cover panic/error boundaries,
writer capabilities and hostile mutation of the response ID. The new public
consumer test adds the previously missing actual cross-service evidence.

## Executed verification

Linux ARM64, Go 1.27.1, isolated source worktree and local HTTP listeners only:

```sh
cd tests/telemetryconsumer
go test -race -count=1 -v ./...
go vet ./...
golangci-lint run ./...
```

Race passed six named tests/subtests, zero skips; vet passed; pinned lint reported
zero issues. The external module pins its own dependencies and uses a test-only
relative replacement for the exact candidate. Root manifests are unchanged.
Formatter and diff checks passed. The test must be run explicitly: the parent
module's `go test ./...` does not enter a nested module.

Two compiled mutations in disposable source demonstrated regression sensitivity:

1. Reusing the incoming ID instead of generating it failed the explicit
   caller-correlation reuse assertion.
2. Adding incoming authorization to log attributes failed bearer-redaction
   assertions, including failed requests.

After each mutation, original implementation bytes were restored and the same
race test passed. Final remote application source equals Git; committed fixture
bytes match the executed candidate. No shared checkout or live service mutated.

## Acceptance boundary and delivery

This qualifies T10.3's original bounded request logging/privacy and App-to-business
service correlation criteria. A second independent AMOS ingress intentionally
generates a different internal ID; arbitrary upstream IDs do not override it.
No parent-trace field, live provider, production private-proxy identity, network,
mTLS, deployment, worker metrics or E16 qualification is implied.

Requested batch ceiling10; actual member1, integrator-owned acceptance with a
bounded proof implementer and independent final review. Original author claim
and historical receipts remain intact. Session/model selection is inherited;
exact token/cost telemetry is unavailable. No acceleration or hosted CI claim.
Independent exact-head review approved `f09933d488f110a7ebcfe9eb3fb11ae2135f7f8d`
against `ff97e730167a3fe03091f8bb441b1fbf8f7625a8`, including all eight files,
original task criteria and frozen trust contracts. PR #121 rebased to
`0208b87c7bc24d058d327a08d5634c6ef77af4ee`; the entire landed tree matches
the reviewed candidate and is reachable from main. Fresh landed full telemetry
race and six external-consumer race checks passed, zero skips (consumer1.032s).
The own supplemental qualification claim was released; original author claims
were preserved. No hosted CI checks were configured.
