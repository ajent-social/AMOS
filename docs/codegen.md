# Contract validation and generation foundation

The repository contract baseline is OpenAPI 3.1.1. T2.1 uses the pinned
`github.com/pb33f/libopenapi` v0.38.7 parser/model builder and validates AMOS
operation metadata before producing a deterministic generation manifest.
Upstream describes support for OpenAPI 3.1 and publishes the selected release
and its MIT license in the [project repository](https://github.com/pb33f/libopenapi),
[v0.38.7 release](https://github.com/pb33f/libopenapi/releases/tag/v0.38.7), and
[license](https://github.com/pb33f/libopenapi/blob/v0.38.7/LICENSE). The
parser's broader 3.1/3.2 support does not change AMOS's exact 3.1.1 requirement.

The root CLI registers `internal/cli/generate.Run` under `amos generate` when
the integrator adds the executable entrypoint. The handler accepts
`--spec <file>` and optional `--manifest <file>`. Without `--manifest`, it
writes the stable JSON manifest to standard output. A manifest output is
written via a same-directory temporary file and atomic rename; an existing
file is replaceable only when it already carries AMOS's generated-manifest
format marker. Validation happens before any output write. The manifest sorts
operations by stable `operationId` and includes the SHA-256 digest of exact
input bytes.

Every operation needs a globally unique `operationId` and `x-amos-policy` with
exactly the fields from `api/policy.schema.json`. Empty permission or
entitlement requirements must be written as `[]`; omission is rejected.
Unknown policy keys, invalid names, duplicate names, unknown enum values, and a
policy `operationId` that differs from the OpenAPI operation are errors.

The foundation bounds source documents to 4 MiB, YAML nesting to 128 levels,
and references to 4,096. It rejects YAML aliases and merge keys, duplicate YAML
keys, webhooks, callbacks, Path Item references, cyclic references, unresolved
references, and every non-internal reference. The parser is configured with
file and remote reference resolution disabled; only canonical local JSON
Pointers are considered. These restrictions avoid reading arbitrary local
files or making network requests during validation. Parser diagnostics are
reduced to stable error categories and operation/JSON-Pointer locations so raw
contract values are not echoed.

T2.1 emits a validated manifest; it does not emit Go transport source. The
typed transport generator is a later task and must consume this validated
operation representation, preserve the same write-ownership rule, and add
meaningful generated-code tests before claiming transport generation.
