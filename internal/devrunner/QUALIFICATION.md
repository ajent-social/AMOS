# Local runner evidence

This package supervises a native application binary and an isolated, labelled
Podman database. It does not supply a generated application or qualify cloud
hosting. The database volume is retained; cleanup targets the exact owned
container and process group, and removes only a network created by this run.

The integrator exercised the real PostgreSQL container through host TCP with
the generated runtime role and database, migration command, graceful stop,
startup timeout, migration and server failures, retained volume, and preservation
of an unrelated container. The complete lifecycle suite passed. Readiness
requires authenticated SQL, rather than only a listening port or container-local
socket. Credentials are environment inputs; captured diagnostics redact them.

Independent source review found a consumed completion notification could hang
cleanup and an early-exiting parent could leave an owned child process alive.
The integrator replaced the notification with broadcast completion, bounded
process-group shutdown and retained exit status. The regression failed when
failed-startup cleanup was genuinely disabled, reporting the surviving child;
the source was restored and the regression plus graceful lifecycle test passed.
Scoped vet and lint passed after all changes.

The configured native binary remains required. CLI wiring and generated-app
acceptance are separate; no missing binary is represented as a runnable app.
