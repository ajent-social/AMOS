# Dependency inventory

Status: inventory of the integrated AMOS module, recorded 2026-09-30. The module is defined by [`go.mod`](../go.mod), with module content checksums in [`go.sum`](../go.sum). The checksums below pin the module contents selected by Go's module graph; version tags or pseudo-version commits identify upstream source. License links point to the corresponding upstream license files.

## Selected toolchain and modules

| Item | Origin and license | Selected version / immutable evidence | Verdict |
|---|---|---|---|
| Go toolchain | Official Go distribution; BSD-3-Clause ([license](https://go.dev/LICENSE)) | Module minimum `go 1.27.0`; selected toolchain `go1.27.1`. Local `go version` reported `go1.27.1 darwin/arm64`. Official [Go 1.27.1 download/checksum listing](https://go.dev/dl/) gives SHA-256 `ee215d57e0ec269c60cc9ceca68e6bda321ba9ee5afe24f4b0988703c2d87d12` for `go1.27.1.darwin-arm64.tar.gz`; see also the [1.27 release notes](https://go.dev/doc/go1.27). | **Selected.** This is the project toolchain; the module's Go directive states its minimum. |
| Go standard library | Bundled with Go; BSD-3-Clause ([license](https://go.dev/LICENSE)) | Go 1.27.1 toolchain | **Selected baseline.** No separate module pin. |
| `github.com/jackc/pgx/v5` | [`jackc/pgx`](https://github.com/jackc/pgx); MIT ([v5.11.0 license](https://github.com/jackc/pgx/blob/v5.11.0/LICENSE)) | `v5.11.0`; `go.sum` content hash `h1:IzBBtyK9AHqf98cctWFifYSci2hgQR/cd56wB4p+ogg=` | **Selected direct PostgreSQL driver.** The module graph and checksum are recorded in the root manifests. Deployment requirements include PostgreSQL in both supported hosting shapes. |
| `github.com/jackc/pgpassfile` | [`jackc/pgpassfile`](https://github.com/jackc/pgpassfile); MIT ([v1.0.0 license](https://github.com/jackc/pgpassfile/blob/v1.0.0/LICENSE)) | `v1.0.0`; `go.sum` content hash `h1:/6Hmqy13Ss2zCq62VdNG8tM1wchn8zjSGOBJ6icpsIM=` | **Selected indirect module** in the pgx graph. |
| `github.com/jackc/pgservicefile` | [`jackc/pgservicefile`](https://github.com/jackc/pgservicefile); MIT ([license at selected source commit](https://github.com/jackc/pgservicefile/blob/5a60cdf6a761/LICENSE)) | `v0.0.0-20240606120523-5a60cdf6a761`; source commit `5a60cdf6a761`; `go.sum` content hash `h1:iCEnooe7UlwOQYpKFhBabPMi4aNAfoODPEFNiAnClxo=` | **Selected indirect module** in the pgx graph. The pseudo-version resolves to the commit shown. |
| `github.com/jackc/puddle/v2` | [`jackc/puddle`](https://github.com/jackc/puddle); MIT ([v2.2.2 license](https://github.com/jackc/puddle/blob/v2.2.2/LICENSE)) | `v2.2.2`; `go.sum` content hash `h1:PR8nw+E/1w0GLuRFSmiioY6UooMp6KJv0/61nB7icHo=` | **Selected indirect module** in the pgx graph. |
| `golang.org/x/sync` | [`golang/sync`](https://github.com/golang/sync); BSD-3-Clause ([v0.17.0 license](https://github.com/golang/sync/blob/v0.17.0/LICENSE)) | `v0.17.0`; `go.sum` content hash `h1:l60nONMj9l5drqw6jlhIELNv9I0A4OFgRsG9k2oT9Ug=` | **Selected indirect module** in the pgx graph. |
| `golang.org/x/text` | [`golang/text`](https://github.com/golang/text); BSD-3-Clause ([v0.29.0 license](https://github.com/golang/text/blob/v0.29.0/LICENSE)) | `v0.29.0`; `go.sum` content hash `h1:1neNs90w9YzJ9BocxfsQNHKuAT4pkghyXc4nhZ6sJvk=` | **Selected indirect module** in the pgx graph. |

## Qualification notes

The module graph is now concrete: pgx is selected directly and its five indirect modules are pinned and checksummed. Upstream license evidence is linked per selected version/source. Preserve each license and copyright notice when distributing the resulting binary or source; the project's Apache-2.0 license does not replace these third-party terms.

The dependencies are inventoried and provenance-qualified for source, version and license. Runtime behavior still requires the project integration checks and a real PostgreSQL database; this document does not claim those checks ran or establish production/security maturity. The T1.6 public-artifact checker is tracked separately and is not certified by this inventory.

## UUIDv7 identifier dependency

`github.com/google/uuid` v1.6.0 is selected for canonical UUID parsing and trusted UUIDv7 generation. The exact module checksum is `h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=`. Its [pinned implementation](https://github.com/google/uuid/blob/v1.6.0/version7.go) and [BSD-3-Clause license](https://github.com/google/uuid/blob/v1.6.0/LICENSE) were inspected. UUIDs are identifiers, not authentication secrets; current authority still comes from verified credentials and database relationships. Consumer tests must enforce canonical lower-case text, version 7, and the RFC variant.

## Override HTML validation

The override renderer uses the Go project HTML5 parser from pinned
`golang.org/x/net v0.59.0` to validate active POST forms and their body CSRF
fields, excluding inert template content and foreign namespaces. It serves the
parsed representation used by that decision. The module checksum and BSD
license notice are recorded in the dependency lock and third-party notices.
[Package documentation](https://pkg.go.dev/golang.org/x/net@v0.59.0/html).

## Frontend verification formatter

Verification tooling pins Prettier 3.9.9 and Playwright 1.63.0 in the browser
package manifest and lockfile. Prettier uses MIT licensing; its distribution
notice is preserved. These are development tools, not application runtime
dependencies. Run the pinned local formatter for changed browser/frontend
verification files.
