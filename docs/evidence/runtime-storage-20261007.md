# Runtime-only storage source checkpoint

[PR26](https://github.com/ajent-social/AMOS/pull/26) remains a draft at
`79d3014c85c1a5884927bcff05f3e664b4f101a9`, based on
`64ba2c8494fdef39da72a53df2bfcb495f79ec1d`. The candidate changes only
`storage/runtime.go` and `storage/runtime_test.go`. It is not merged or accepted.

A separate final Astra reviewer cleared the complete v1.15 source after fixes.
Independent normal and race runtime tests explicitly excluded the required-service
case; scoped vet and pinned golangci-lint v2.13.2 passed with zero issues. The
required-service invocation without configuration failed visibly. Those results
are not real PostgreSQL evidence.

## Findings and corrections

The initial review required real TLS/runtime-role coverage. The author added
required-service tests for DML, rollback, panic, cancellation, close, wrong CA and
hostname, and denied schema, temporary-table, migration-ledger and role operations.

Follow-up review reproduced malformed-first/valid-second PEM acceptance: the
standard parser could skip an invalid block before finding a valid one. Strict
first-block parsing now rejects that input and nested BEGIN markers; a valid
multi-certificate bundle remains accepted. The final reviewer temporarily
substituted the old parser, observed both regressions fail, restored exact source
bytes, and observed the unit suite pass. A pool-wait test now uses a fresh deadline
for BeginTx instead of a context already expired by PingContext.

Reviewed source SHA-256:

- `storage/runtime.go`: `90f0d4b9cff4a36b66482dae53f1b00ea99e3ab548b0dcd80b0114ace450dac5`
- `storage/runtime_test.go`: `62cb378ead1f70fc7c40d62c1f290178919fb784afa8ab20633dffc3d315e812`

Publication scanning produced five matches in synthetic test literals, an invalid
PEM marker, and source assignments. Manual inspection found no real credentials or
key material; automated scanner success is not claimed.

## Blocking service gate

The independently reviewed local test-fixture trial failed before PostgreSQL
startup. Its owned resources were removed and cleanup was checked. No qualified
connection configuration or actual TLS/runtime-role evidence was produced. The
single authorized trial was consumed; further trial authority is not inferred.
Fixture error classification and retained startup diagnostics require correction
and independent review before another concrete trial can be proposed.

INT-HOST-03 verification, full review admission, merge, landed verification and
source acceptance remain open. Unit fixtures do not satisfy them. Existing
accepted development storage work is unchanged. Production grants, TLS endpoints,
service adapters, host composition, provider behavior and deployment remain
separate gates.
