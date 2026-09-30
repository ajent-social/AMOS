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
