# Pure continuity guide source evidence

Date: 2026-10-08. Scope: application-owned pure guide component only.

## Exact delivery boundary

The independently reviewed guide design at
`f11806200383e472c86b30e19ba762b295248f21` landed through PR56 at
`6287e9b5e653ca115d7391f3182ea2b965e7a75e`; both design files match.
Source and tests were independently reviewed at
`0ff5df62c4ccb5133fa2f64b7c6581e5b4af6e87` and landed through PR59 at
`0df0347b3922da351dc45783846a92b3ccabf39f`. All three package files match the
reviewed Git blobs. The reviewer did not author this source.

The package validates the entire supplied selection, bounded canonical IDs and
values, positive revisions through MaxInt64, original-body SHA-256 and resolvable
source references before producing an answer. Five finite question scenarios
return deterministic plain text with independent sorted reference slices. Owner
updates reuse the pure domain draft function and remain unsaved, unsent previews.
It performs no I/O, model call, permission decision or external action.

## Actual checks

The author passed scoped normal/race tests, vet and pinned golangci-lint 2.13.2.
The independent reviewer freshly ran:

- `go test -json -count=1 ./examples/continuity/guide`
- `go test -race -json -count=1 ./examples/continuity/guide`
- `go vet ./examples/continuity/guide`
- Pinned `golangci-lint` 2.13.2 on the package.

Each independent normal/race run produced 91 pass events: 90 test/subtest results
plus the package result, with zero skips and zero failures. Vet and lint passed.
The scoped public-artifact check passed. No required service exists for this
pure package; these are not database, browser or provider results.

The author bypassed the source digest guard and observed its intended failed
assertion, restored exact bytes and passed the regression. Independently, the
reviewer removed the missing-reference guard and the digest guard in separate
isolated mutations. Both failed their named TestSelectionValidation assertion
requiring zero output and the exact error. Restoring the original production file
passed the full 91 events again; its SHA-256 was
`f5b5f00748ec89e37e20538b6906ccd28c5e8f89979e728016b550320c08de96`.

After merge, a fresh `go test -json -count=1 ./examples/continuity/guide` on the
exact landed revision passed all 91 events with zero skips. The source, test
and README Git blobs were compared directly with the reviewed head.

## Limits

This records the six T-RPL-GUIDE component lifecycle stages only. Original
product accepted-task count is unchanged. Hash consistency proves neither truth
nor authority. Caller-supplied selectors are not proof of current session,
workspace or resource permission. Persistence, source disclosure, HTTP, SSR,
browser behavior, production providers and replacement rehearsal remain separate
unqualified gates. Full SaaS, billing, generator and deployment scope is retained.
