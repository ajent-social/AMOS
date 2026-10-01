-- Billing state is workspace owned and scoped to one provider account/mode.
-- The integrator assigns this fragment's global migration sequence.
-- This fragment is additive. After billing rows exist, schema correction is
-- forward-only; do not drop these tables or replay external provider effects.

CREATE TABLE billing_workspace_accounts (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    installation_id uuid NOT NULL CHECK (get_byte(uuid_send(installation_id), 6) >> 4 = 7 AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (get_byte(uuid_send(application_id), 6) >> 4 = 7 AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (get_byte(uuid_send(environment_id), 6) >> 4 = 7 AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL,
    provider text NOT NULL CHECK (provider ~ '^[a-z][a-z0-9_-]{0,63}$'),
    provider_account_id text NOT NULL CHECK (provider_account_id ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'),
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','suspended','closed')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_workspace_accounts_scope_key UNIQUE (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_workspace_accounts_workspace_fk FOREIGN KEY (workspace_id, installation_id, application_id)
        REFERENCES workspaces (id, installation_id, application_id),
    CONSTRAINT billing_workspace_accounts_workspace_provider_key
        UNIQUE (installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
);

CREATE TABLE billing_customer_bindings (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    billing_account_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    customer_ref text NOT NULL CHECK (customer_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','retired')),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    retired_at timestamptz,
    CONSTRAINT billing_customer_account_fk FOREIGN KEY (billing_account_id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
        REFERENCES billing_workspace_accounts (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_customer_state_time CHECK ((state = 'active' AND retired_at IS NULL) OR (state = 'retired' AND retired_at IS NOT NULL))
);
CREATE UNIQUE INDEX billing_customer_active_per_workspace_idx ON billing_customer_bindings (billing_account_id) WHERE state = 'active';
CREATE UNIQUE INDEX billing_customer_provider_ref_idx ON billing_customer_bindings (installation_id, environment_id, provider, provider_account_id, account_mode, customer_ref) WHERE state = 'active';
CREATE INDEX billing_customer_workspace_idx ON billing_customer_bindings (installation_id, application_id, workspace_id, created_at DESC);

CREATE TABLE billing_provider_intents (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    billing_account_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    operation text NOT NULL CHECK (operation ~ '^[a-z][a-z0-9_.-]{0,95}$'),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','claimed','unknown','confirmed','failed')),
    claim_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    provider_object_ref text CHECK (provider_object_ref IS NULL OR provider_object_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    last_audit_reference_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_intent_account_fk FOREIGN KEY (billing_account_id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
        REFERENCES billing_workspace_accounts (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_intent_claim_shape CHECK ((state = 'claimed' AND claim_token IS NOT NULL AND lease_until IS NOT NULL) OR (state <> 'claimed' AND claim_token IS NULL AND lease_until IS NULL)),
    CONSTRAINT billing_intent_idempotency_key UNIQUE (billing_account_id, operation, idempotency_key)
);
CREATE INDEX billing_intent_lease_idx ON billing_provider_intents (lease_until, created_at) WHERE state = 'claimed';
CREATE INDEX billing_intent_workspace_idx ON billing_provider_intents (installation_id, application_id, workspace_id, updated_at DESC);

CREATE TABLE billing_intent_audit_references (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    intent_id uuid NOT NULL REFERENCES billing_provider_intents (id),
    audit_reference_id uuid NOT NULL CHECK (get_byte(uuid_send(audit_reference_id), 6) >> 4 = 7 AND substring(audit_reference_id::text, 20, 1) IN ('8','9','a','b')),
    transition text NOT NULL CHECK (transition IN ('created','claimed','reclaimed','confirmed','unknown','failed')),
    intent_version bigint NOT NULL CHECK (intent_version > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_intent_audit_reference_unique UNIQUE (intent_id, audit_reference_id)
);
CREATE INDEX billing_intent_audit_page_idx ON billing_intent_audit_references (intent_id, created_at, id);

CREATE FUNCTION billing_intent_audit_references_are_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'billing intent audit references are append only' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    NEW.created_at := transaction_timestamp();
    RETURN NEW;
END $$;
CREATE TRIGGER billing_intent_audit_reference_guard BEFORE INSERT OR UPDATE OR DELETE
    ON billing_intent_audit_references FOR EACH ROW EXECUTE FUNCTION billing_intent_audit_references_are_append_only();
CREATE TRIGGER billing_intent_audit_reference_truncate_guard BEFORE TRUNCATE
    ON billing_intent_audit_references FOR EACH STATEMENT EXECUTE FUNCTION billing_intent_audit_references_are_append_only();

CREATE TABLE billing_provider_inbox (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    billing_account_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    provider_event_ref text NOT NULL CHECK (provider_event_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    state text NOT NULL DEFAULT 'received' CHECK (state IN ('received','processing','processed','failed')),
    received_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    processed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CONSTRAINT billing_inbox_account_fk FOREIGN KEY (billing_account_id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
        REFERENCES billing_workspace_accounts (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_inbox_processing_time CHECK ((state IN ('received','processing') AND processed_at IS NULL) OR (state IN ('processed','failed') AND processed_at IS NOT NULL))
);
CREATE UNIQUE INDEX billing_inbox_event_unique ON billing_provider_inbox (installation_id, environment_id, provider, provider_account_id, account_mode, provider_event_ref);
CREATE INDEX billing_inbox_pending_idx ON billing_provider_inbox (received_at, id) WHERE state IN ('received','failed');

CREATE TABLE billing_subscription_projections (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7 AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    billing_account_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
    subscription_ref text NOT NULL CHECK (subscription_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    state text NOT NULL CHECK (state IN ('pending','unknown','confirmed','failed','canceled','past_due','incomplete')),
    price_key text NOT NULL CHECK (price_key ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
    quantity bigint NOT NULL CHECK (quantity >= 0),
    period_start timestamptz,
    period_end timestamptz,
    observed_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT billing_projection_account_fk FOREIGN KEY (billing_account_id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode)
        REFERENCES billing_workspace_accounts (id, installation_id, application_id, environment_id, workspace_id, provider, provider_account_id, account_mode),
    CONSTRAINT billing_projection_period_shape CHECK (period_start IS NULL OR period_end IS NULL OR period_end > period_start),
    CONSTRAINT billing_projection_provider_ref_unique UNIQUE (billing_account_id, subscription_ref)
);
CREATE UNIQUE INDEX billing_projection_provider_object_unique ON billing_subscription_projections (installation_id, environment_id, provider, provider_account_id, account_mode, subscription_ref);
CREATE INDEX billing_projection_workspace_idx ON billing_subscription_projections (installation_id, application_id, workspace_id, observed_at DESC);
