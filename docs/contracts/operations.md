# Operations contract

Status: proposed task contract under frozen `amos-contract-v1`. This document
specifies target and evidence fields; it does not claim that backups, recovery,
metrics, alerts, or version endpoints are implemented or qualified.

## Observable signals

| Signal | Required fields and meaning |
|---|---|
| Liveness | `status` (`ok` or `failed`), `checked_at` UTC RFC 3339. It answers whether this process can serve; it must not imply dependencies are ready. |
| Readiness | `status` (`ready` or `unavailable`), `checked_at`, and bounded dependency names/states. Never include diagnostic payloads or imply future availability. |
| Version | `version`, `source_revision`, `artifact_digest`, `built_at`; unknown values are explicit `unknown`, not inferred. |
| Request correlation | Server-generated `request_id` echoed in response header/body. Client values are untrusted and never become authority. |
| Structured error | Stable `code`, human-safe `message`, `request_id`; optional bounded `retry_after_seconds`. No raw provider/database detail. |
| Worker | `worker`, `state` (`idle`, `running`, `waiting`, `failed`), `observed_at`, `last_success_at` (nullable), `attempt`, `queue_age_seconds` (nullable). No job payloads or credentials. |

Current runtime behavior is narrower: `/healthz` returns `{"status":"ok"}`;
`/readyz` checks configured dependencies and returns a generic unavailable
error; responses use server-generated request IDs and structured errors. No
version endpoint or worker telemetry is implemented by this task.

## Owner-configurable targets

`internal/opsconfig` supplies **PROPOSED** defaults only: availability SLO
99.0%, RPO 24 hours, RTO 72 hours, retention 30 days, backup interval 24 hours,
and CPU, memory, and disk alert thresholds of 80%. An installation owner must
choose `aws_managed` or `aws_vm`, name the alert, recovery-credential, and
incident-action owners, adjust values to their needs, validate the complete
configuration, and explicitly set `Activated` before treating any target as
selected. These values create no availability or recovery promise.

RPO, RTO, retention, backup interval, profile, and named owners are required;
unset, invalid, or unknown values fail validation. Availability SLO must be
finite and strictly between 0 and 100. Owner names containing only whitespace
are treated as unset. Retention must be at least the RPO. Backup interval
greater than RPO is denied, since that schedule cannot
support the declared data-loss target. Resource thresholds must be in (0,100].
This package defines validation only; it is not yet wired into application
startup or a backup scheduler.

The installation owner assigns a person or role for alert response, custody and
rotation of recovery credentials, and incident decisions/actions. The contract
does not assume a hosted on-call provider. Credentials remain in the owner's
secret manager and are never serialized into operations evidence.

## Evidence schema and claim boundary

Targets and observations are different records. Target fields are configuration
values above. A measurement is represented separately:

```json
{
  "profile": "aws_managed",
  "measured_at": "2026-10-01T00:00:00Z",
  "result": "passed",
  "source": "owner-reviewed measurement reference",
  "scope": "named installation and measurement window"
}
```

`profile`, UTC `measured_at`, `result`, `source`, and `scope` are required;
`result` describes an observed outcome and is not copied from a target. A
promise may be emitted only when targets are valid, a profile is explicitly
selected, activation is explicit, and evidence is complete, timestamped, and
for that same profile. This package provides an evidence shape only; it has no
trusted measurement source and makes no promise decision. Reviewers must
establish that a referenced measurement is genuine and relevant. Empty or
fabricated strings are not evidence. No measured result, provider
qualification, restore drill, deployment, or operational readiness is claimed
here.

Database, object/file, and deployment recovery remain separate capabilities.
Restoring identity state must not silently resurrect revoked credentials.
Profile-specific targets require actual measurements for the chosen profile;
evidence for one profile cannot qualify another.
