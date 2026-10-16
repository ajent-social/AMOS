# T14.2 local diagnostic projection component

Base: `f6c3f40bc211865d5132fe09551161cb0db10392`. The two source/test files
were executed against T14.1 main and rebased onto metrics-only main without
changing their bytes. This is a bounded useful component; T14.2 remains in
progress while truthful runtime aggregate/source admission is unresolved.

The constructor parses at most64KiB under explicit depth/token/array/object/string
limits, rejects duplicate decoded keys and maps only exact approved metric/error
names and unsigned counts into fresh canonical bucketed fields. Unknown metadata,
raw/base64 messages, URLs, input versions and proof claims are never copied.
All frames are omitted, including spoofed public symbols/digests/trust booleans:
no qualified executing-build provenance adapter exists. There is no regex
redaction, unsafe placeholder or caller-provided approval callback.

Output is a copied local preview, self-checked by the finite diagnostic contract,
with version/hash receipt marked `unqualified-local-preview`. Constants describe
the target candidate vocabulary, not the installed component or measured24h
provenance. A receipt proves only local byte/profile validation. It is not export
eligibility, consent, source authenticity or trustworthy aggregate collection.
No sender, external transport, storage or listener is introduced.

## Executed verification

Linux ARM64, Go1.27.1, isolated source and local fixture bytes only:

```sh
go test -race ./internal/diagnosticsanitize -count=1
go vet ./internal/diagnosticsanitize
golangci-lint run ./internal/diagnosticsanitize
go test ./internal/diagnosticsanitize -run '^$' -fuzz '^FuzzSanitization$' -fuzztime=5s -parallel=1
```

Full scoped race/vet/lint passed with zero skips/issues. Bounded fuzzing completed
48,163 executions. The synthetic corpus includes private labels, stack arguments
and home paths, URL queries, base64 messages, nested data and unknown versions.
Actual gzip compression and decompression checks assert no canary in serialized
or transport-fixture bytes. This is a transport fixture, not a network/live
provider or deployed privacy qualification.

A disposable compiled mutation replaced omitted frames with a public-looking
placeholder. `TestSanitizationFreshProjectionAndCompressedPrivacy` failed with
`unqualified frame admitted`. Exact source bytes restored; full race passed.
Committed files matched executed bytes. Formatter/diff/public-safe checks passed.
No on-disk external service receipt was fabricated from tool-session evidence.

## Remaining gate and delivery

Qualified executing-component provenance, real aggregate/window truth and actual
public-frame admission remain absent. Do not widen this preview receipt into a
trusted exporter input or accept the full task from this component. Owner consent,
spool/retention, external destination and transmission remain separate tasks.

Requested batch ceiling10; one coherent local component delivered alongside
independent disjoint work. Inherited model/settings preserved; exact cost/token
telemetry unavailable. Final independent source review, guarded merge and fresh
landed checks remain required; no hosted CI or acceleration claim.
