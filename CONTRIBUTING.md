# Contributing to AMOS

AMOS is public software. Read [AGENTS.md](AGENTS.md), the current task, the
frozen contract, and the relevant architecture decisions before proposing a
change. Keep private source details, credentials, personal paths, infrastructure
identifiers, and customer data out of issues, commits, tests, fixtures, and
generated artifacts.

## Changes and evidence

- Keep changes within the assigned task and owned paths. Preserve existing work
  and use an isolated worktree for changes that can discard or overwrite state.
- Explain the user-visible behavior and contract affected. Include focused
  tests and the exact commands and results that support any claim.
- Tests using fixtures or mocks establish only the behavior they exercise; they
  do not qualify a live provider, deployment, or production environment.
- Treat generated files as generated: identify the generator and version, make
  the source-of-truth change, regenerate, and review the diff. Do not hand-edit
  generated output unless the task explicitly requires it.
- Do not add dependencies without recording their source, exact version,
  license, integrity evidence, and selection rationale in the dependency
  inventory.

## Contributions and licensing

By intentionally submitting a contribution for inclusion, you agree that it is
provided under the Apache License, Version 2.0, unless a separate written
agreement says otherwise. Submit only work you authored or have permission to
contribute, and identify material copied or adapted from other sources with its
license and provenance. Generated contributions are subject to the same
authorship, provenance, and license checks as handwritten work; disclose the
generator and review generated output before submission.

## Review

Reviewers should verify contract compatibility, tests, public-artifact hygiene,
dependency and notice changes, and the evidence behind claims. A review or
passing fixture suite does not imply security review, provider qualification,
or production readiness unless the corresponding evidence is recorded.
