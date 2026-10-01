-- Additive normalized, verified endpoint inbox; raw bodies and secrets are excluded.
CREATE TABLE billing_verified_webhook_ingress (
 id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id),6)>>4=7 AND substring(id::text,20,1) IN ('8','9','a','b')),
 installation_id uuid NOT NULL,
 application_id uuid NOT NULL,
 environment_id uuid NOT NULL,
 provider text NOT NULL CHECK (provider ~ '^[a-z][a-z0-9_-]{0,31}$'),
 provider_account_id text NOT NULL CHECK (provider_account_id ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
 account_mode text NOT NULL CHECK (account_mode IN ('test','live')),
 provider_event_ref text NOT NULL CHECK (provider_event_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
 event_type text NOT NULL CHECK (event_type ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
 customer_ref text NOT NULL CHECK (customer_ref='' OR customer_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
 subscription_ref text NOT NULL CHECK (subscription_ref='' OR subscription_ref ~ '^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$'),
 payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256)=32),
 billing_account_id uuid,
 workspace_id uuid,
 state text NOT NULL CHECK (state IN ('received','quarantined')),
 quarantine_reason text NOT NULL,
 received_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(installation_id,application_id,environment_id,provider,provider_account_id,account_mode,provider_event_ref),
 FOREIGN KEY(billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode)
 REFERENCES billing_workspace_accounts(id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode),
 CHECK ((state='received' AND billing_account_id IS NOT NULL AND workspace_id IS NOT NULL AND quarantine_reason='' AND customer_ref<>'') OR
        (state='quarantined' AND billing_account_id IS NULL AND workspace_id IS NULL AND quarantine_reason IN ('unknown_customer','metadata_mismatch','unsupported_event','missing_customer','account_mismatch')))
);
CREATE INDEX billing_verified_ingress_pending_idx ON billing_verified_webhook_ingress(installation_id,application_id,environment_id,state,received_at,id);
