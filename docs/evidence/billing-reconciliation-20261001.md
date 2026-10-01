# Billing reconciliation components: 2026-10-01

Source batch `ccc119c` composes the durable core, immutable provider-price
lookup and pinned official Stripe SDK snapshot source.

Independent headless re-review cleared the expired-retry blocker and found no remaining standalone component source blocker. The reviewer ran no tests; the integrator obtained the checks below.

## Obtained checks

- Fresh `go test -race ./billing/reconcile ./billing/stripecheckout ./billing/catalog -count=1` passed against required PostgreSQL; scoped vet passed and pinned lint reported zero issues.
- Real database regressions reject expired apply and retry leases even when the caller transaction began before expiry. Predicate-removal mutants fail the intended regressions and restored checks pass.
- A serialization-removal mutant detects concurrent provider calls from one reconciler instance; restoration passes.
- SDK fixtures cover complete bounded pagination, double-read changes, wrong subscription mode, unknown prices and lifecycle shapes, and generic errors. Removing mode protection triggers its regression.
- Removing SDK diagnostic suppression triggers the captured-output regression; restoring byte-identical source makes the full SDK/catalog scope pass. Catalog alias mutation similarly fails and restored catalog checks pass.

## Qualification boundary

Fixtures are not live Stripe. Required live Stripe credentials are absent in the
current environment. Two normalized provider reads detect changes but cannot
prove an atomic upstream snapshot. Request spacing and serialization are local
to one reconciler instance; account-wide throttling is not implemented.
Historical prices must remain in the configured immutable catalog. The lookup
is for reconciliation and does not authorize checkout or grant entitlements.

T5.8 remains in progress: durable webhook consumer wiring, executable scheduling,
shared migration allocation, live provider qualification and integrated
subscription/access behavior remain separate. The schema fragment has no
claimed application sequence. No deployment or complete release qualification
is inferred.
