# Source and publication provenance

Recorded 2026-09-30. This file records the origin and clearance status of AMOS-authored material and selected dependencies. It contains only public references and public-safe status; no restricted source locator, donor content, or private evidence is reproduced.

## AMOS-authored work

The owner selected **Apache License 2.0** for original AMOS code. This records the decision; adding the canonical license text and applying the notice to source files are release/integration follow-ups. The canonical terms are available from the [Apache Software Foundation](https://www.apache.org/licenses/LICENSE-2.0). This selection does not change third-party licenses or grant rights to material outside AMOS's authorized source set.

## Third-party and shared-source register

| Source | Intended use | License / version evidence | Clearance and publication verdict |
|---|---|---|---|
| Go toolchain and standard library | Project language/runtime foundation | Module minimum Go 1.27.0, toolchain 1.27.1; Go project [BSD-3-Clause license](https://go.dev/LICENSE), official [release listing](https://go.dev/dl/) | **Selected and qualified for provenance.** No Go source was copied into AMOS by this task. |
| `github.com/jackc/pgx/v5` | PostgreSQL driver | Selected v5.11.0, MIT; module checksum and versioned license link in [dependency inventory](dependencies.md) | **Selected direct dependency.** No upstream source was vendored or copied by this documentation task. Runtime checks remain integration evidence. |
| `github.com/jackc/pgpassfile`, `github.com/jackc/pgservicefile`, `github.com/jackc/puddle/v2` | Transitive pgx modules | Selected versions and MIT licenses; pgservicefile is pinned to source commit `5a60cdf6a761`; checksums and versioned license evidence in [dependency inventory](dependencies.md) | **Selected indirect dependencies.** Preserve upstream license and copyright notices in distributions. |
| `golang.org/x/sync`, `golang.org/x/text` | Transitive pgx modules | Selected versions and BSD-3-Clause licenses; checksums and versioned license evidence in [dependency inventory](dependencies.md) | **Selected indirect dependencies.** Preserve upstream license and copyright notices in distributions. |
| `ajent-social/go`, `ajent-social/pulumi`, `ajent-social/capabilities`, `ajent-social/workflows` | Possible AMSL contracts/components/workflows | Public [Go](https://github.com/ajent-social/go/blob/main/LICENSE), [Pulumi](https://github.com/ajent-social/pulumi/blob/main/LICENSE), [capabilities](https://github.com/ajent-social/capabilities/blob/main/LICENSE) and [workflows](https://github.com/ajent-social/workflows/blob/main/LICENSE) license files are Apache-2.0. Per-repository reuse boundaries are in [reuse matrix](reuse-matrix.md). No exact version or immutable revision is selected. | **Candidates only; not imported.** License is known, but public availability alone does not select a version or qualify an AMOS consumer. |
| Restricted donor source | No current use | No source details are included here | **Blocked/excluded.** Import and publication clearance has not been granted. No donor file was copied or adapted in this task. |

## Evidence and limits

Evidence used for this inventory consists of the checked-in AMOS module manifest and checksum file, the local `go version` result, public versioned upstream license files, and public repository license files for the AMSL candidates. The selected Go module content hashes are committed in `go.sum` and repeated in [dependency inventory](dependencies.md); pgservicefile is also tied to its pseudo-version commit. AMSL candidate links use the public `main` branch only to establish current license text; no AMSL version is selected. Record the exact AMSL revision and complete notices before any future import. The T1.6 public-artifact checker is tracked separately and was not run for this documentation task.

No AMSL or restricted donor source code was copied, and no third-party notice was removed or altered. These records make no claim of security maturity, production fitness, or successful AMOS integration.

## UUIDv7 identifier dependency

`github.com/google/uuid` v1.6.0 is selected for canonical UUID parsing and trusted UUIDv7 generation. The exact module checksum is `h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=`. Its [pinned implementation](https://github.com/google/uuid/blob/v1.6.0/version7.go) and [BSD-3-Clause license](https://github.com/google/uuid/blob/v1.6.0/LICENSE) were inspected. UUIDs are identifiers, not authentication secrets; current authority still comes from verified credentials and database relationships. Consumer tests must enforce canonical lower-case text, version 7, and the RFC variant.


## Validation caller qualification history (2026-10-16)

The earlier shared-source inventory is supplemented only for the generated CI
caller candidate: public `ajent-social/workflows` was inspected at immutable revision
`601745b22e78dbed4a7349c7dc3fad2cc2b4c0dd`, using
[go-validation.yml](https://github.com/ajent-social/workflows/blob/601745b22e78dbed4a7349c7dc3fad2cc2b4c0dd/.github/workflows/go-validation.yml)
and [go-lint.yml](https://github.com/ajent-social/workflows/blob/601745b22e78dbed4a7349c7dc3fad2cc2b4c0dd/.github/workflows/go-lint.yml).
The pinned [Apache-2.0 license](https://github.com/ajent-social/workflows/blob/601745b22e78dbed4a7349c7dc3fad2cc2b4c0dd/LICENSE)
was inspected; no upstream workflow source is copied into AMOS. All other AMSL
component/version decisions remain separate; the MCP baseline gate is unchanged.

Both public workflows and all five distinct transitive action commits were
resolved through public repository APIs. Checkouts disable credential
persistence; setup-go disables its cache. The lint action's own cache remains
on, keyed by platform/workdir/time interval/go.mod with a broader restore prefix.
The reusable lint workflow exposes no cache-disable input. This caller uses it
only for unprivileged PR and merge-group validation with read-only permissions;
no privileged cache consumer or release/artifact trust is qualified here.

The upstream workflows remain CANDIDATE. Ordinary reviewed consumer source
selection does not promote upstream maturity or prove hosted GitHub execution.
Local generated-output, aggregate-script and qualification checks belong to the
[T9.2 delivery record](planning/ship-validation-caller-20261016.md).


The proposed CI pin is not functionally adopted: actual command qualification
found the upstream package-selection defect recorded in the T9.2 delivery note.
No generated caller should be advertised as qualified until a reviewed correction
and new immutable pin pass the actual consumer checks.


The proposed repair is [public upstream PR #5](https://github.com/ajent-social/workflows/pull/5)
at `f7b7326b6e86d381ced145080355a2eeea8e19b7`. Its actual nested-module
command regression fails against the original and passes the correction;
independent review is clear, but human maintainer review and merge are pending.
This proposed head is not an adopted AMOS pin. Detailed source/test boundaries
remain in the T9.2 delivery record; no consumer or upstream maturity is promoted.


## Resumed with owner-approved upstream correction

The owner explicitly approved the correction in
[upstream PR #5](https://github.com/ajent-social/workflows/pull/5). Its guarded
rebase merge landed `680caa87f92395cb034df6a9b2dc95b517134209`; the complete
landed tree equals independently reviewed `f7b7326b6e86d381ced145080355a2eeea8e19b7`.
The generated caller now pins the landed SHA for both validation and lint.
The earlier human-review blocker is resolved. T9.2 is IN_PROGRESS pending fresh
exact-source consumer verification, independent AMOS review and landing.
The candidate was safely rebased onto current main while preserving accepted
T12.2 implementation, records and authored scope. CANDIDATE upstream maturity,
cache limits and hosted/provider/deployment qualification boundaries are unchanged.


T9.2 subsequently completed independent candidate review and guarded landing in
[AMOS PR149](https://github.com/ajent-social/AMOS/pull/149), with fresh actual
generated-consumer behavior checks. Its qualification is limited to generated
caller/local commands; the prior blocked/resumed notes are historical. Upstream
workflows remain CANDIDATE and no hosted/provider/deployment maturity is promoted.
