# Generated dashboard and alert qualification

Source base: `72319109ce26e8f4966ee3963406dda87ef15534`. Five source/test
files implement local deterministic rendering and evaluation; no module, migration,
provider, notification or listener change. Source was tested on Linux ARM64 with
Go1.27.1 and rebased across disjoint upstream configuration changes without
altering executed source bytes.

```sh
go test -race ./internal/scaffold/ops ./cmd/amos -count=1 -json
go vet ./internal/scaffold/ops ./cmd/amos
golangci-lint run ./internal/scaffold/ops ./cmd/amos
```

Fifty named checks passed, zero skips. Vet passed; lint reported zero issues.
The full CLI suite includes its existing actual PostgreSQL regression. Actual
built CLI render produced seven panels/rules/runbooks in 5,571 bytes; evaluation
replayed pending, pending, firing, recovering, recovered. Sending stays false.
Readable duration strings and canonical snake_case targets are the config wire;
artifact numeric durations explicitly use `_ns` suffixes.

A compiled RPO comparison reversal made the backup freshness regression fail;
exact restored source passed. Final restored dashboard SHA256:
`1e8157b3b49d0b5b20fe84c0d782f24478a141a8b4b41bb950edab7033ef95af`.
A second concrete RED case showed float conversion losing a queue threshold
crossing at 2^53+1; exact uint64 comparison now passes that regression. Gaps reset
failure/recovery holds; missing, stale and reset inputs cannot resolve incidents.
Disk/backup/certificate observations remain explicit separate inputs; generic
worker success is never treated as successful backup publication.

Requested batch ceiling10; one coherent source member. Inherited model/settings
preserved; exact token/cost telemetry unavailable. Independent final review,
guarded merge, reviewed-tree equality and fresh landed checks are required.
No hosted CI, deployed collector, actual provider behavior, notification delivery,
SLO/RPO achievement, production or release admission is claimed.

PR #128 landed at `05d13493bd2824bec3c7d06b6e4de08becaf2135` with full
reviewed-tree equality. The premerge public-artifact scan flagged a synthetic
unknown-field canary assignment; command sequencing incorrectly allowed merge
after that failure. A reviewed follow-up renames the fixture key to `unexpected`
without weakening the scanner or changing the rejection test. It contained no
real credential. Fresh landed execution and scanner results are recorded in
the follow-up integration record.
