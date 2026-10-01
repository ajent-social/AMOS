-- Envelope ciphertext only; keys and plaintext TOTP seeds never enter SQL.
CREATE TABLE identity_totp_factors (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id),6)>>4=7 AND substring(id::text,20,1) IN ('8','9','a','b')),
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL CHECK (get_byte(uuid_send(environment_id),6)>>4=7 AND substring(environment_id::text,20,1) IN ('8','9','a','b')),
    person_id uuid NOT NULL,
    seed_ciphertext bytea NOT NULL CHECK (octet_length(seed_ciphertext) BETWEEN 32 AND 4096),
    seed_format smallint NOT NULL CHECK (seed_format=1),
    state text NOT NULL CHECK (state IN ('pending','active','expired')),
    pending_security_epoch bigint,
    last_used_step bigint NOT NULL DEFAULT -1,
    failed_attempts smallint NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
    locked_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz,
    activated_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    FOREIGN KEY (person_id,installation_id,application_id)
        REFERENCES identity_persons(id,installation_id,application_id),
    CHECK (
        (state='pending' AND pending_security_epoch IS NOT NULL AND pending_security_epoch>=0 AND last_used_step=-1 AND expires_at>created_at AND expires_at<=created_at+interval '30 minutes' AND activated_at IS NULL)
        OR (state='active' AND pending_security_epoch IS NULL AND last_used_step>=0 AND expires_at IS NULL AND activated_at IS NOT NULL)
        OR (state='expired' AND pending_security_epoch IS NULL AND expires_at IS NULL AND activated_at IS NULL)
    )
);
CREATE UNIQUE INDEX identity_totp_one_pending_idx ON identity_totp_factors(installation_id,application_id,environment_id,person_id) WHERE state='pending';
CREATE UNIQUE INDEX identity_totp_one_active_idx ON identity_totp_factors(installation_id,application_id,environment_id,person_id) WHERE state='active';
CREATE INDEX identity_totp_expired_pending_idx ON identity_totp_factors(expires_at,id) WHERE state='pending';
