# Dependency inventory

Status: documentation qualification, recorded 2026-09-29. No application source, `go.mod`, lockfile, or generated dependency inventory exists in this checkout. This is a prospective inventory, not a claim that AMOS has implemented or tested these components.

## Selected foundation

| Item | Origin and license | Version | Evidence and verdict |
|---|---|---|---|
| Go toolchain | Official Go distribution; Go source and standard library are BSD-3-Clause ([license](https://go.dev/LICENSE)) | **1.27.1**, confirmed by `go version` (`darwin/arm64`) | Official [release downloads](https://go.dev/dl/) list 1.27.1 and checksums; official [1.27 release notes](https://go.dev/doc/go1.27). **Selected as the project toolchain.** This command confirms the local toolchain only; no AMOS build is claimed. |
| Go standard library | Bundled with the Go distribution; same Go project BSD-3-Clause terms | Go 1.27.1 | Official [Go license](https://go.dev/LICENSE) and [standard library reference](https://pkg.go.dev/std). **Selected baseline**, with no separate module or version pin. Actual package use awaits implementation. |

## Proposed, not selected

| Item | Origin and license | Version | Evidence and verdict |
|---|---|---|---|
| PostgreSQL Go driver (`github.com/jackc/pgx/v5`) | Public upstream [`jackc/pgx`](https://github.com/jackc/pgx); [MIT license](https://github.com/jackc/pgx/blob/master/LICENSE) | Upstream changelog reports v5.10.0 (2026-06-03); AMOS has no pin | The [upstream README](https://github.com/jackc/pgx) documents pgx as a PostgreSQL driver/toolkit and its support policy. **Candidate only.** Decide driver/API, supported PostgreSQL range, TLS/authentication policy, pool limits and integration evidence; then pin an exact version and inspect its full transitive licenses. |

## Qualification rule

Do not convert a candidate into a selected dependency based on a repository name, an upstream branch, or a planning recommendation. Selection requires an exact module/component version or immutable source revision, license/notice review including transitive code, and evidence from the actual AMOS consumer boundary. Until a dependency manifest and consumer checks exist, no database driver, web framework, migration tool, frontend package, infrastructure package, or external service client is qualified here.

The minimum repository test for the next implementation step is to add a module manifest, pin the selected versions, preserve third-party notices, and run the dependency/license inventory against that manifest. This task did not execute a project checker; `scripts/check-public-artifacts.py` is a future prescription in T1.4, not an available or run check.
