# Source and publication provenance

Recorded 2026-09-29. This file records the origin and clearance status of AMOS-authored material and proposed reuse. It contains only public references and public-safe status; no restricted source locator, donor content, or private evidence is reproduced.

## AMOS-authored work

The owner selected **Apache License 2.0** for original AMOS code. This records the decision; adding the canonical license text and applying the notice to source files are release/integration follow-ups. The canonical terms are available from the [Apache Software Foundation](https://www.apache.org/licenses/LICENSE-2.0). This selection does not change third-party licenses or grant rights to material outside AMOS's authorized source set.

## Third-party and shared-source register

| Source | Intended use | License / version evidence | Clearance and publication verdict |
|---|---|---|---|
| Go toolchain and standard library | Project language/runtime foundation | Go 1.27.1 confirmed locally; Go project [BSD-3-Clause license](https://go.dev/LICENSE), official [release listing](https://go.dev/dl/) | **Cleared for documenting the public toolchain.** No Go source was copied into AMOS by this task. |
| `jackc/pgx` | Possible PostgreSQL driver | Public [upstream repository](https://github.com/jackc/pgx), [MIT license](https://github.com/jackc/pgx/blob/master/LICENSE), changelog entry for v5.10.0 | **Not adopted or copied.** Candidate pending a pin, transitive notice review and consumer qualification. |
| `ajent-social/go`, `ajent-social/pulumi`, `ajent-social/capabilities`, `ajent-social/workflows` | Possible AMSL contracts/components/workflows | Public repository links in [reuse matrix](reuse-matrix.md); immutable version/revision and complete license evidence are not established in this inventory | **Not imported.** Public availability alone is not a selected version or AMOS consumer qualification. Inspect file-level provenance and notices before any future import. |
| Restricted donor source | No current use | No source details are included here | **Blocked/excluded.** Import and publication clearance has not been granted. No donor file was copied or adapted in this task. |

## Evidence and limits

Evidence used for this inventory consists of the checked-in AMOS task/vision/boundary documents, the local `go version` result, and the official/public upstream pages linked above. Upstream branch pages and changelogs can change; they are evidence for identification and reported license/version only, not immutable supply-chain pins. Before release, replace branch-based references with exact reviewed tags/commits and checksums where applicable, record complete direct and transitive notices, and run the repository's public-artifact checks. The prescribed T1.4 checker was not present in this checkout and was not run.

No source code was copied, no third-party notice was removed or altered, and no claim of security maturity, production fitness, or successful AMOS integration is made by this document set.
