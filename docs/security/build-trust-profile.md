# AMOS build trust profile (design contract)

Status: design contract only. This profile records a consumer policy and the
shape of evidence a future release verifier must require. It does not create
attestations, verify cryptography at AMOS runtime, qualify GitHub Actions,
prove branch protection, or certify a release.

## Profile version and specifications

The evidence schema uses JSON Schema Draft 2020-12. Provenance uses the SLSA
Build Provenance v1 predicate (`https://slsa.dev/provenance/v1`) inside an
in-toto Statement v1. SLSA 1.2 is the currently approved specification version
reviewed for this profile; its build provenance format and requirements are
the normative references. A format version does not confer any SLSA level or
builder qualification. See the [SLSA 1.2 specification](https://slsa.dev/spec/v1.2/),
[build provenance](https://slsa.dev/spec/v1.2/build-provenance), and
[in-toto attestation framework](https://github.com/in-toto/attestation).

The proposed producer is GitHub artifact attestations using
`actions/attest` v4.2.2. The verifier is GitHub CLI `gh attestation verify`
v2.102.0. Those are explicitly selected reference versions as of 2026-10-10;
this repository currently has no release workflow that installs, pins, or runs
them. Before adoption, the action must be pinned to a reviewed full commit SHA
per the accepted CI trust contract, and the CLI binary/toolchain must be
pinned by digest. The action tag is not an immutable workflow pin. GitHub
published gh v2.102.0 on 2026-09-30 and its release notes include fixes for
attestation signer-workflow matching and source-ref case sensitivity. See the
[gh release](https://github.com/cli/cli/releases/tag/v2.102.0),
[actions/attest v4.2.2](https://github.com/actions/attest/releases/tag/v4.2.2),
[GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations),
and [`gh attestation verify`](https://cli.github.com/manual/gh_attestation_verify).

This profile selects Build L2 as the current evidence target: GitHub
artifact attestations alone provide SLSA v1.0 Build L2. Build L3 is not selected
or claimed. An L3 upgrade requires a separately reviewed, SHA-pinned reusable
workflow that contains the build steps and generates the attestation; until that
workflow exists, evidence from the caller workflow remains L2. For any future
L3 profile, the verifier must pin the reusable workflow's repository, exact path,
and immutable revision as the signer, separately from the caller workflow.
The generic `slsa-github-generator` workflow is not selected: its maintained
upstream documentation marks it no longer actively maintained and directs new
integrations to GitHub artifact attestations.

## Expected source, builder, and artifact bindings

A verifier policy must be supplied independently of the evidence and bind all
of the following:

- Repository: exact canonical `https://github.com/ajent-social/AMOS`.
- Source: full immutable source commit, exact protected release tag ref, and
  source repository identity. Branch, pull-request, fork, and untrusted refs
  cannot satisfy a release policy.
- Caller workflow: exact `.github/workflows/release.yml` identity and the
  workflow revision/digest. This is the current Build L2 signer. Do not claim
  Build L3 from this path. A future L3 profile must independently pin the
  reusable workflow repository, complete path, and immutable revision as the
  signer; constrain the caller workflow separately.
- Builder: for the selected GitHub-hosted runner profile, require SLSA
  `builder.id` exactly `https://github.com/actions/runner/github-hosted`, in
  addition to the GitHub Actions certificate identity and signer workflow
  allowlist. Reject self-hosted or other builder IDs until a separately
  reviewed profile is approved. Record runner class and run invocation without
  treating a run number as identity.
- Subject: the verified statement's SHA-256 subject digest must equal the
  artifact bytes being released. The provenance source URI and commit must
  match the independently selected source revision.
- Inputs: record the exact Go version, action/tool versions, resolved
  dependencies, workflow revision and their immutable digests. Version strings
  without content digests are descriptive only.
- Policy: bind the release policy identifier and digest plus the exact required
  check set. A green check name without its trusted workflow/run context is not
  evidence of a trusted build.

The schema requires the source repository/ref/commit, builder/workflow/revision,
artifact digest, toolchain input digests, policy digest, provenance digest,
signer identity, issuer, expected audience and verifier checks. Its semantic
validator compares repository, allowed ref, builder and signing identity,
issuer, audience and verifier version against a separately supplied policy.
Schema acceptance and string comparison do not verify signatures or cross-check
cryptographic subject/predicate contents; the future release gate must use the
pinned verifier against the artifact and attestation bundle and inspect the
verified result.

A reference online check is:

```sh
gh attestation verify ./AMOS-release.tar.gz \
  --repo ajent-social/AMOS \
  --signer-workflow ajent-social/AMOS/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z \
  --source-digest FULL_SOURCE_COMMIT_SHA \
  --signer-digest FULL_WORKFLOW_FILE_COMMIT_SHA \
  --predicate-type https://slsa.dev/provenance/v1 \
  --format json
```

The caller must additionally require the exact source digest and signer
workflow digest supported by the pinned CLI, compare the artifact subject
SHA-256 to the downloaded file, and apply the policy/check-set digest. Do not
accept a successful signature check alone. `gh attestation verify` documents
checking actor identity, signer workflow, predicate type, and artifact
integrity; predicate content remains workflow-controlled metadata and needs
trusted-builder policy checks.

## Signing identity, issuer, audience, and trust roots

For public AMOS artifacts the accepted issuer is exactly
`https://token.actions.githubusercontent.com`; the accepted certificate
identity is the complete, policy-approved workflow identity, never a substring
or email-like display name. The OIDC token audience is the exact recipient
configured for the attestation signing exchange (`sigstore`); it must be
requested for that exchange and not reused as an unconstrained deployment
credential. GitHub's OIDC provider documents issuer,
audience, subject, repository, workflow, and immutable identity claims. A
consumer must use `gh attestation verify`'s issuer and identity checks; the
recorded audience expectation is issuance policy metadata and is not independently
recovered from a verified artifact bundle by that CLI. Do not claim a later
consumer cryptographically proved the transient audience claim.

Trust roots are GitHub's Sigstore public-good trusted root for this public
repository, including the Fulcio certificate authority and Rekor transparency
log keys distributed by the official trusted-root mechanism. Do not embed
long-lived signing keys or manually trust an arbitrary certificate. Rotation
uses fresh root material from `gh attestation trusted-root`; review and record
the root document SHA-256 with each verification. Fail closed if root refresh,
certificate identity/issuer, signature, transparency proof, expected subject,
or policy check fails. Treat the signing certificate as short-lived; trust the
issuer and timestamped certificate chain under the selected roots, not a copied
leaf certificate or embedded public key. If a supported emergency revocation signal or root
integrity cannot be obtained, stop release verification pending explicit
security review; do not silently fall back to signature-presence checks.

For online verification, fetch the attestation and current trusted roots over
the verifier's authenticated HTTPS path and require transparency-log inclusion.
For offline verification, import the artifact, bundle, pinned verifier and a
recently refreshed trusted-root document through a separately controlled
transfer. Record its digest and verification time. Offline verification can
check only against that snapshot: GitHub documents that an old trusted-root
file will not reveal later key revocation. Consequently offline verification
is a bounded historical check, not proof of current revocation state, and must
be rejected when policy requires current online status.

## Rebuild and reproducibility policy

Do not claim bit-for-bit reproducibility until independent clean builds on the
supported operating systems produce identical artifact digests. Until then,
require a separately trusted provenance statement, exact dependency/toolchain
input digests, source/workflow binding, independent review of build differences,
and the exact policy/check set. Differences must be enumerated and approved;
missing or unexplained inputs reject the artifact. Rebuilding from source is
not an independent check if the same mutable workflow, runner, or dependency
resolution controls both builds.

## Evidence and limitations

`api/schemas/release-evidence.schema.json` defines the bounded record. Tests
exercise structural requirements and a correctly shaped but unapproved
source-ref/signing policy negative. These are schema and semantic-policy
fixtures; no signed artifact or GitHub workflow ran for this task. The current
T9.1 output is a design contract only: generated workflow wiring, protected
environments, real signing, provider behavior, live GitHub checks, release
verification, and deployed operation remain separate qualification gates.
The `gh attestation verify` CLI version itself must be rechecked at each future
release integration and updated for security fixes before using this profile.

## Primary references reviewed 2026-10-10

- [SLSA v1.2](https://slsa.dev/spec/v1.2/) and [Build Provenance](https://slsa.dev/spec/v1.2/build-provenance).
- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations) and [offline verification/trusted roots](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline).
- [GitHub Actions OIDC reference](https://docs.github.com/en/actions/reference/security/oidc).
- [Go 1.27.0 downloads and official archive checksums](https://go.dev/dl/).
- [GitHub-hosted builder ID reference in SLSA verifier guidance](https://github.com/slsa-framework/slsa-verifier/blob/main/README.md).
- [`gh attestation verify` manual](https://cli.github.com/manual/gh_attestation_verify), [gh v2.102.0 release](https://github.com/cli/cli/releases/tag/v2.102.0), and [`actions/attest` v4.2.2](https://github.com/actions/attest/releases/tag/v4.2.2).
- [Sigstore public-good verification model](https://docs.sigstore.dev/cosign/verifying/verify/) and [Cosign bundles](https://docs.sigstore.dev/cosign/signing/signing_with_blobs/); these describe the underlying signature and transparency model, not a claim that Cosign is AMOS's selected verifier.
