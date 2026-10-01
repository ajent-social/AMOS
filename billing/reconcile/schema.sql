-- Durable reconciliation work is per scoped provider customer. Work versions
-- make concurrent webhook signals invalidate an in-flight provider read.

CREATE TABLE billing_reconcile_work (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    billing_account_id uuid NOT NULL,
    customer_binding_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    customer_ref text NOT NULL CHECK (customer_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:./-]{0,127}$'),
    dirty_version bigint NOT NULL DEFAULT 1 CHECK (dirty_version > 0),
    claim_version bigint,
    claim_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_scan_cycle bigint NOT NULL DEFAULT 0 CHECK (last_scan_cycle >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    last_observed_at timestamptz,
    last_provider_revision text,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_reconcile_scope_fk FOREIGN KEY (billing_account_id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
        REFERENCES billing_workspace_accounts (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_reconcile_customer_fk FOREIGN KEY (customer_binding_id)
        REFERENCES billing_customer_bindings (id),
    CONSTRAINT billing_reconcile_claim_shape CHECK ((claim_token IS NULL AND claim_version IS NULL AND lease_until IS NULL) OR (claim_token IS NOT NULL AND claim_version IS NOT NULL AND lease_until IS NOT NULL)),
    CONSTRAINT billing_reconcile_claim_version CHECK (claim_version IS NULL OR claim_version <= dirty_version),
    CONSTRAINT billing_reconcile_one_per_account UNIQUE (billing_account_id)
);
CREATE INDEX billing_reconcile_due_idx ON billing_reconcile_work (next_attempt_at, updated_at, id)
    WHERE claim_token IS NULL;
CREATE INDEX billing_reconcile_lease_idx ON billing_reconcile_work (lease_until, id)
    WHERE claim_token IS NOT NULL;

-- Provider lifecycle vocabulary is recorded separately from the existing
-- entitlement projection states. This preserves facts such as trialing and
-- suspended without changing catalog policy or silently granting access.
CREATE TABLE billing_reconcile_projection_metadata (
    billing_account_id uuid NOT NULL REFERENCES billing_workspace_accounts(id),
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    subscription_ref text NOT NULL CHECK (subscription_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:./-]{0,127}$'),
    provider_status text NOT NULL CHECK (provider_status IN ('active','trialing','past_due','suspended','canceled','incomplete','unpaid','expired','absent')),
    provider_revision text NOT NULL CHECK (provider_revision ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'),
    observed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (billing_account_id,subscription_ref),
    CONSTRAINT billing_reconcile_metadata_scope_fk FOREIGN KEY (billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode)
        REFERENCES billing_workspace_accounts (id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode)
);

CREATE TABLE billing_reconcile_ingress (
    ingress_id uuid PRIMARY KEY REFERENCES billing_verified_webhook_ingress (id),
    work_id uuid NOT NULL REFERENCES billing_reconcile_work (id),
    linked_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE INDEX billing_reconcile_ingress_work_idx ON billing_reconcile_ingress (work_id,linked_at);

CREATE TABLE billing_reconcile_scan_checkpoints (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    after_account_id uuid,
    cycle bigint NOT NULL DEFAULT 0 CHECK (cycle >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_reconcile_scan_scope_key UNIQUE (installation_id, application_id, environment_id, provider, provider_account_id, account_mode)
);
