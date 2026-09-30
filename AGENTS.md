# AMOS agent instructions

- AMOS is public: keep private source details, personal paths, credentials, infrastructure identifiers, and customer information out of tracked artifacts.
- Use Go for application code and all Pulumi infrastructure programs. The default UI uses server-rendered HTML, HTMX, and plain JavaScript/CSS.
- Read `docs/RESUME.md`, the frozen contract, and the assigned task before implementation. Preserve execution evidence in `docs/planning/execution-state.json`.
- Work in isolated task worktrees; the integrator owns shared contracts, module files, migration ordering, executable wiring, and plan progress unless explicitly delegated.
- Use real required-service tests, fail visibly when prerequisites are absent, and never claim fixtures qualify live providers.

## Existing permission grants

When the user grants full access or approves commands for the current task, honor that grant throughout the session. Continue authorized work autonomously; do not repeatedly ask for confirmation for routine commands, local commits, tests, or other actions already covered by the grant. Check the actual tool permission mode and use existing approvals. If the runtime enforces escalation, request it only as required by that runtime; do not add a separate conversational approval question or treat an already approved action as awaiting user intent. A permission grant does not authorize unrelated scope, override higher-priority restrictions, or permit destructive changes to other people's work.
