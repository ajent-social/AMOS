-- Proposed jobs fragment. Root migration ordering and registry wiring remain integrator-owned.
CREATE TABLE amos_jobs (
    id uuid PRIMARY KEY,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    kind text NOT NULL,
    payload jsonb NOT NULL,
    external_effect boolean NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'leased', 'succeeded', 'dead', 'unknown')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts > 0),
    reconciliation_attempt_count integer NOT NULL DEFAULT 0 CHECK (reconciliation_attempt_count >= 0),
    max_reconciliation_attempts integer NOT NULL CHECK (max_reconciliation_attempts > 0),
    manual_review boolean NOT NULL DEFAULT false,
    available_at timestamptz NOT NULL,
    next_reconciliation_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    lease_owner text,
    lease_action text CHECK (lease_action IS NULL OR lease_action IN ('execute', 'reconcile')),
    fence_token bigint NOT NULL DEFAULT 0 CHECK (fence_token >= 0),
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (installation_id, application_id, idempotency_key),
    CHECK ((lease_owner IS NULL) = (lease_until IS NULL)),
    CHECK ((lease_owner IS NULL) = (lease_action IS NULL))
);

CREATE INDEX amos_jobs_ready_idx ON amos_jobs (available_at, created_at, id)
    WHERE status = 'queued';
CREATE INDEX amos_jobs_reconcile_idx ON amos_jobs (next_reconciliation_at, created_at, id)
    WHERE status = 'unknown' AND manual_review = false;
CREATE INDEX amos_jobs_expired_lease_idx ON amos_jobs (lease_until, id)
    WHERE lease_owner IS NOT NULL;
