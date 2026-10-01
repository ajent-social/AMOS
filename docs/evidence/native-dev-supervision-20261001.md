# Native development supervision: 2026-10-01

The local runner composes the generated native application with real Podman
PostgreSQL without requiring a Compose provider. Private installation UUIDs
identify owned resources; cleanup checks immutable resource IDs and current
labels, preserves the data volume, and never adopts slug-only resources.

## Obtained evidence

- Before the final process-state lock correction, fresh race tests passed for
  devrunner, CLI and application packages (35.851, 2.014 and 42.056 seconds).
  Actual generated native application/runner/PostgreSQL/Chromium lifecycle
  passed. Scoped vet passed and pinned lint reported zero issues.
- Ownership-label and runtime database environment mutations produced the
  intended regression failures; byte-identical restoration passed scoped checks.
- Independent headless review identified concurrent Record/Clear lost updates;
  the correction serializes complete state mutations through a persistent
  private rooted lock. Independent follow-up source review found no blocker.

- After integrating frozen lock fix `6379750` as `3ad9f03`, fresh three-package
  race tests passed (35.443, 1.645 and 41.539 seconds), including the actual
  generated native application/runner/PostgreSQL/Chromium lifecycle. Scoped
  vet passed and pinned lint reported zero issues. Worker native/Linux arm64
  vet/lint and filtered Linux status tests passed; the Clear-lock bypass
  mutation failed and restored scoped tests passed.

- Public-artifact false positives in synthetic environment/OS credential
  fixtures were resolved by explicit fixture construction; no scanner bypass
  was added. The changed regression scope passed race (1.350 seconds), vet
  and zero-issue lint. Public artifact and planning scans pass.

## Limits

T7.7 remains in progress. CLI clean --plan is advisory and refuses execution;
no cleanup executor acceptance is claimed. Linux status/probe checks are scoped,
not full Linux runner qualification. Browser evidence exercises the actual
runner API, not an interactive CLI command. Local captured email and database
checks do not qualify live providers, cloud deployment or a complete release.
