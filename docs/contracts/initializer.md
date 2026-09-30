# Initializer contract v1

`amos init` creates an owner-controlled Go SaaS repository from versioned templates. This document and its case inventory are inputs to implementation, not evidence that the command exists. Local evaluation requires no cloud account or provider credentials. Deployment is a distinct command with explicit validated installation configuration.

## Input and configuration

Interactive and noninteractive modes produce the same validated schema-versioned input: app slug, Go module path, parent directory and relative target directory, enabled supported modules, public origin, integrated-Go or private-service business mode, and optional explicit managed or small-vm AWS deployment profile. Go/Pulumi remain Go in both profiles. Cloudflare is required for an AWS production target. A local profile is cloud-disabled, loopback-only and visibly lacks qualified external-provider features; this mode never simulates billing or identity providers.

The app slug is a lowercase DNS-style identifier of 1–63 characters. Module paths follow the maintained Go module validator, prohibit whitespace/control characters and credentials, and are distinct from public origins. Origins contain scheme plus authority only: HTTPS for production, HTTP only for canonical loopback evaluation; no userinfo/query/fragment. Domain ownership, AWS region/account, Cloudflare zone/DNS and self-hosted Pulumi-state details are validated separately as owner configuration. Private identifiers and paths do not appear in public generated example data.

Supported module identifiers are versioned registry entries. Unknown modules, missing dependencies and unsupported combinations fail before writes. Disabled modules do not expose successful placeholder operations. Default SaaS capabilities can be generated while external method/provider activation remains visibly unqualified until setup succeeds. A private-service extension uses an authenticated origin-bound internal routing contract; it never exposes that origin as the user's business URL.

No password, service key, credential, token, raw connection string or encrypted-secret ciphertext is an initializer argument, JSON field, journal or ownership-manifest value. Secret settings are opaque references resolved through owner-controlled adapters; environment input is referenced by name, never copied into public output. Prompts use protected input only in a separate qualified enrollment flow. Diagnostics redact provider payloads and never echo supplied sensitive values.

## Path and ownership boundaries

Input selects an existing parent and a relative target. Reject absolute targets, empty/dot targets, traversal and symlink escapes after resolving ancestors. An existing target is rejected unless an explicit resume record proves this exact interrupted generation owns it. `--force` does not authorize overwriting authored directories. Generation does not traverse a symlink to write outside the selected parent. Parent and target checks are repeated at finalization to detect replacement/races; fail safely if ownership cannot be established.

Generate in a uniquely named sibling staging directory on the same filesystem. Use create-exclusive files with restrictive permissions for private local configuration. Validate generated formatting, template/schema version, path manifest and required local checks before finalization. Write the ownership manifest and durable resume journal before atomically renaming staging into the absent final target. Atomic rename failure leaves a resumable owned staging directory rather than a partially generated target. Cross-filesystem copy is not a silent fallback.

The public ownership manifest records template/component versions, relative generated paths, content digests and ownership mode (managed or authored). It excludes home paths, hostnames, installation account identifiers, secrets and arbitrary tool output. Authored business code and page overrides are preserved. Managed upgrades later use preimage/three-way merges; init is not an overwrite command.

Resume records are private, schema-versioned, bound to the specific target/staging identity and template digest, and track completed write/validation/finalization steps. Recovery checks actual preimages and stops on authored modifications, unknown paths or incompatible versions. Never delete an unowned path while recovering. A cancellation before finalization leaves a documented private resume marker; cancellation after finalization reports the resulting created repository rather than pretending it did not exist.

## CLI output and exit behavior

Human mode writes concise status to stdout and sanitized diagnostics to stderr. JSON mode emits one schema-versioned final envelope with operation, status, relative target label, component versions, warnings, next required steps and stable error code; progress stays on stderr. It does not print credentials or raw tool output. `success` means files finalized and checks passed, not live deployment, paid checkout, working external sign-in or enterprise readiness.

Exit 0 means generation succeeded; 2 means input/schema/version/path conflict; 3 means prerequisite or enabled capability unavailable; 4 means owned generation/validation failure; 5 means explicitly resumable interrupted state; 130 means user cancellation, with recovery state documented separately. Unknown schema major versions fail before filesystem mutation. Cancellation honors context deadlines, closes handles and avoids deleting unowned material. Exact schema compatibility is pinned to the generated CLI/template manifest.

`amos deploy` must consume the owner-selected profile and validated references, show plan/provider prerequisites and expose real deployment outcomes. It does not infer AWS/Cloudflare account choices from initializer success. Profile migration is an explicit workflow, not changing a string on a running installation.

## Contract cases and rejected outcomes

The machine-readable cases cover valid local and production-target inputs, unknown modules, traversal, absolute paths, existing authored targets, interruption/resume and schema mismatch. Validators must reject invalid cases before writing. Interruption tests run only in owned temporary directories and verify journals/preimages rather than terminating unrelated processes. A resume fixture with modified authored content must preserve it and fail visibly. Initializer implementation supplies an executable input schema and command registration to the integrator; this contract does not authorize competing CLI entrypoints.
