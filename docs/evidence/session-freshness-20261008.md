# Post-lock session freshness component

PR54 landed `4dec7f7efbdfad73a7b6d5276c3301d910813f45` from independently
reviewed `6c2bb6e508496b3420ade1ca74937df358dfade3`, against the adopted
[v1.21 contract](../contracts/session-renewal-freshness.md). The author and
reviewer were different sessions. Only the scoped lookup, assurance lookup and
new required-service tests changed; all three landed blobs match reviewed source.

Renewal locks the scoped session row before sampling database time, rejects
unsupported isolation and tests the old lifetime before extending idle expiry.
Assurance samples fresh time and downgrades expired proof. Scope, exported
signatures and optional assurance middleware wiring are unchanged.

## Obtained verification

The independent reviewer ran these checks against a fresh, bounded, real TLS
PostgreSQL runtime-only fixture with two connections:

- `go test -count=1 -json -run '^TestSessionFreshnessRequiredService' ./identity/store`
- `go test -race -count=1 -json -run '^TestSessionFreshnessRequiredService' ./identity/store`
- `go vet ./identity/store`
- Pinned `golangci-lint` 2.13.2 on `./identity/store`.

Normal and race each passed 27 subtests, three parent tests and the package
(31 pass events), with zero skips. Vet and lint passed. The tests observe the
exact blocking backend and database clock crossing, covering idle/absolute
expiry, live renewal, rollback, assurance downgrade, revocation, epoch/state
changes, realm/digest denial, cancellation and unsupported/completed transactions.

The author demonstrated four intended negative cases: transaction-start renewal,
transaction-start assurance, a clock sampled before the lock, and treating exact
assurance expiry as live. Independently regenerated reviewer overlays reproduced
old renewal, old assurance and equality failures. Exact original files remained
unchanged, and the restored full selection passed. A fresh landed run of that
selection passed with the same 31 pass events and zero skips.

Exact-equality probes deliberately position an owned boundary at a real database
sample through a test-only SQL-forwarding facade. They are not evidence that a
naturally scheduled request hit that instant. Separate unmodified two-connection
schedules prove actual lock waiting and elapsed database boundaries.

## Limits

This qualifies the bounded freshness correction, not full person/session writer
ordering, provenance, current-principal handoff, operation authority, production
roles or providers. T2.8 remains in progress and the accepted-task count does not
change. The optional-schema-absent path was inspected in source; that absence was
not exercised by this runtime profile. Legacy schema-creating integration suites
and the full-repository race suite were outside this fixture grant.

The focused public scanner reports one configuration-field forwarding expression
in the test that forwards the configuration password field. Inspection found
no credential literal;
no scanner rule was suppressed and no full-green public scan is asserted. Hosted
CI was not used as the delivery gate.
