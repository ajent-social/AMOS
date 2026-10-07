# Runtime-only PostgreSQL storage delivery

[PR26](https://github.com/ajent-social/AMOS/pull/26) merged reviewed head
`67e4982486fdbf9839f7e1e625bbe58d4b3efca6`, based on
`ae644026c8fbc55eeb396bf9be9fca22953be509`, as
`f60759556914df571a32193a6cc21af445913bd1`. Both owned files,
`storage/runtime.go` and `storage/runtime_test.go`, equal reviewed bytes on main.
INT-HOST-03 is complete at its source and actual local PostgreSQL boundary.

## Verification and independent review

The author passed 85 runtime test entries in normal and race runs, including 24
required-service entries, with zero skips or failures. Scoped vet and pinned
lint passed. A different reviewer cleared the exact final head and independently
ran the 24 required-service entries normally and with race detection, all passing.
These checks used actual isolated TLS PostgreSQL and a synthetic least-privilege
role: positive DML, rollback, panic, cancellation, close, wrong hostname/root,
and 13 SQLSTATE 42501 denials covering schema, temporary-table, migration-ledger
and role operations. They qualify the local storage boundary, not a production role.

The coordinator repeated the actual required-service suite at the landed revision:
24 run, 24 pass, zero skip/fail. Missing configuration fails visibly. Guarded rebase
merge followed immediate head/base/check readback; no hosted CI success or branch
policy bypass is claimed.

## Findings and corrections

Review required actual-service coverage, strict first-block PEM rejection and a
fresh BeginTx pool-wait deadline. Independent substitution of the old parser made
malformed-prefix and nested-BEGIN regressions fail; exact restored source passed.
Real service execution then found two cancellation assertions using equality where
the frozen contract permits errors.Join with the safe transaction sentinel. The
author changed those assertions to errors.Is without changing production code.
A different reviewer independently reproduced both old failures and cleared the
corrected exact source with actual normal/race service runs.

Earlier fixture attempts failed before startup and their exact-owned resources
were removed. A separately reviewed bounded mount correction allowed the final
local fixture trial to pass actual TLS and role checks. Offline fixture tests
were never substituted for service evidence. Cleanup custody remains with the
fixture operator until its exact-resource cleanup receipt is recorded.

Publication scanner matches in synthetic literals and source expressions were
manually inspected; automated scanner success is not claimed. Existing development
storage acceptance is unchanged. Production grants, endpoints, service adapters,
host composition, provider behavior and deployment remain separate gates.
