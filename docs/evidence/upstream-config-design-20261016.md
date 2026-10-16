# T14.13 upstream configuration design qualification

Source base: `5b8c7c18f21f36daae4d5dd2c6a8ebfe07c43f73`. Owned schema,
contract and checker bytes were tested against metrics main and rebased across
local diagnostic/alert-design changes without modifying those eight files.

The closed `amos-upstream-config-v1` declaration has explicit upstream/consumer
account domains, finite role purposes/namespaces, private normalized quarantine,
KMS/abuse references and bounded default-off collection. Consumer deployment
cannot be an operator. Same declared account is rejected even if ARN strings
differ. This is a narrowly supported requested profile, not actual AWS identity
or inventory evidence; no real account or credential is selected.

Linux ARM64, Go1.27.1, no provider/network credentials or deployment:

```sh
go test -race ./internal/upstreamconfig -count=1
go vet ./internal/upstreamconfig
golangci-lint run ./internal/upstreamconfig
```

Three top-level and34 subtests passed with zero skips; vet passed and pinned
lint reported zero issues. Independent jsonschema4.26.0 Draft2020-12 meta-validation
passed. Its valid fixture is accepted and consumer-operator/public-raw fixtures
are rejected. Shared-account fixture is deliberately schema-valid but fails the
mandatory Go relational check: this difference is documented. Exact recursive
keys/types reject aliases, decoded duplicates and canary overwrites. The rejected
consumer-operator configuration is the design-only negative case; no live IAM
mutation or fabricated service proof is claimed. Formatting/diff/public-safe
checks passed and committed eight-file bytes matched executed source.

This accepts only the original schema/configuration task after independent final
review and guarded landing. Authoritative exclusion inventory and actual IAM,
state, KMS, bucket privacy, TLS/network, MFA operator access, abuse enforcement,
retention/deletion/restore and collection remain unqualified. No ARN inequality,
lexical namespace, requested enabled mode or caller proof can waive those gates.

Requested batch ceiling10; one coherent design/config-validator member alongside
disjoint source work. Inherited model/settings preserved; exact cost/token
telemetry unavailable. Independent review, guarded merge and fresh landed checks
remain required; no hosted CI, acceleration or production claim.

Landed in PR #127 at `72319109ce26e8f4966ee3963406dda87ef15534`,
with full reviewed-tree equality verified. Fresh landed race checks passed all
37 named checks with zero skips; vet passed. These checks qualify only the
declaration validator, not effective account or provider isolation.
