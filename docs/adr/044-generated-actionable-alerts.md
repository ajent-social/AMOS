# ADR044: generate finite owner-routed alert definitions

Status: proposed; independent design review/landing precedes source.

Adopt [amendmentv1.30](../contracts/dashboard-alerts.md) for T10.6. A real local
CLI generates deterministic dashboard/rule/runbook artifacts and evaluates those
same finite rules against explicit measurements. Owner targets remain explicit;
missing/stale/reset signals cannot fabricate health or recover an incident.

Numeric metrics provide HTTP/pool/queue observations. Disk, completed backup and
certificate evidence are separate supplemental inputs; no generic worker success
is a backup. Notifications stay disabled and no sender/provider dependency exists.
This makes the generator executable without selecting a real owner's thresholds,
contact, destination or deployment. Actual monitoring/notification qualification
remains separate from synthetic rule tests and generated definitions.
