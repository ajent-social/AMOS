# Auth integration baseline check repair

Base: `8d00b54b326efa234c4b3aeb6627c797538f0848`.

The complete runtime race suite exposed an older assertion that expected an
encoded billing alias to reach unavailable reserved dispatch (503). Existing
billing ingress deliberately rejects that alias (400), consistent with the
frozen canonical-path contract and the separate billing-handler regression.
The test now explicitly distinguishes canonical unavailable paths (503) from
the rejected encoded alias (400). Application routing and normalization are
unchanged. Both billing HTTP helpers now report response-body close failures
instead of dropping their errors, resolving the two existing lint findings.

On an isolated Linux ARM64 worktree with Go 1.27.1:

- Original routing test restored temporarily: failed for actual400/want503.
  Candidate restored: the same exact test passed.
- Full `go test -race ./internal/runtime -count=1`: passed, 1.056 seconds.
- Actual PostgreSQL `go test -race ./apphost -run '^TestLocal.*Billing'
  -count=1 -v`: three named tests passed, zero skips, 6.144 seconds.
- Full `golangci-lint run ./apphost`: zero issues.
- `gofmt -d` on both changed Go files: empty; `git diff --check`: passed.

The payment adapter in these local billing tests is synthetic. This is source,
routing, lint and actual-database host evidence, not live payment-provider,
full apphost runtime, production or deployment qualification. Other required
TLS/browser prerequisites remain separate and have not been bypassed.

Delivery unit: one bounded maintenance change under an integrator-owned
supplemental claim, independent of retained product-author claims. Requested
batch ceiling10; actual member1. Independent final review and guarded landing
remain required; no hosted CI or acceleration claim.
