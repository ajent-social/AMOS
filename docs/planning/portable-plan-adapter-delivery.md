# AMOS portable plan adapter delivery

Scope: export AMOS's native task definitions and narrative acceptance status to
the experimental portable plan contract 0.0.1. Keep AMOS as the source of
authored truth. This delivery does not qualify execution or deployment.

- [x] Preflight: read AMOS native plan, narrative registry, project contract,
  and frozen portable schema and semantics; use an isolated worktree.
- [x] Implement a deterministic, version-pinned read-only exporter with stable
  AMOS task and domain-acceptance IDs and native task preservation.
- [x] Keep narrative `ACCEPTED`/`REVIEWED` unqualified; emit no execution
  snapshot, evidence, or evaluations.
- [x] Verify focused Go tests, native plan integrity, and frozen schema shape.
- [x] Verify the full 257-task export against the owning offline semantic
  validator from CI run 37184831031 (binary SHA-256
  `f26d7731a0b4f9662dedb7d007ee375236c7dc8f7f3cff9393cf9d0aab59695e`):
  valid, zero findings, `authorityAuthenticated=false`. Final owner handoff
  remains pending.
- [ ] Obtain independent exact-head review, address findings, and reverify.
- [ ] Merge and verify the landed tree and public artifact scan.

The shared contract digest is
`sha256:7582512f122d2f2a9c4461facc7541c9887053f137260d6ebe9c6dea611d039d`.
The adapter is intentionally limited to domain mapping. Receipt trust,
provider checks, canonical execution admission, and deployment qualification
remain separately owned and cannot be inferred from this export.
