CREATE TABLE identity_federation_connections (
    id UUID PRIMARY KEY,
    installation_id UUID NOT NULL,
    application_id UUID NOT NULL,
    provider TEXT COLLATE "C" NOT NULL CHECK (provider IN ('google','github','apple','enterprise_oidc')),
    issuer TEXT COLLATE "C" NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT identity_federation_connections_uuid_v7 CHECK (
        substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_connections_installation_uuid_v7 CHECK (
        substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_connections_application_uuid_v7 CHECK (
        substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_connections_scope_key UNIQUE (id,installation_id,application_id)
);

CREATE FUNCTION identity_federation_connection_tuple_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'federation connection records cannot be deleted';
    END IF;
    IF (NEW.id,NEW.installation_id,NEW.application_id,NEW.provider,NEW.issuer)
       IS DISTINCT FROM
       (OLD.id,OLD.installation_id,OLD.application_id,OLD.provider,OLD.issuer) THEN
        RAISE EXCEPTION 'federation connection identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER identity_federation_connection_tuple_immutable
    BEFORE UPDATE OR DELETE ON identity_federation_connections
    FOR EACH ROW EXECUTE FUNCTION identity_federation_connection_tuple_immutable();

CREATE TABLE identity_federation_flows (
    id UUID PRIMARY KEY,
    state_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(state_digest) = 32),
    browser_digest BYTEA NOT NULL CHECK (octet_length(browser_digest) = 32),
    nonce_digest BYTEA NOT NULL CHECK (octet_length(nonce_digest) = 32),
    provider TEXT COLLATE "C" NOT NULL CHECK (provider IN ('google','github','apple','enterprise_oidc')),
    provider_connection_id UUID NOT NULL REFERENCES identity_federation_connections(id),
    installation_id UUID NOT NULL,
    application_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    issuer TEXT COLLATE "C" NOT NULL,
    intent TEXT NOT NULL CHECK (intent IN ('login','link')),
    person_id UUID REFERENCES identity_persons(id),
    authenticated_at TIMESTAMPTZ,
    security_epoch BIGINT,
    assurance_level TEXT,
    assurance_expires_at TIMESTAMPTZ,
    session_digest BYTEA,
    return_to TEXT NOT NULL CHECK (left(return_to, 1) = '/'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CONSTRAINT identity_federation_flows_uuid_v7 CHECK (
        substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_flows_installation_uuid_v7 CHECK (
        substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_flows_application_uuid_v7 CHECK (
        substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_flows_environment_uuid_v7 CHECK (
        substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT identity_federation_flows_scope_connection_fk
        FOREIGN KEY (provider_connection_id,installation_id,application_id)
        REFERENCES identity_federation_connections (id,installation_id,application_id),
    CONSTRAINT identity_federation_flows_intent_proof CHECK (
        (intent = 'login' AND person_id IS NULL AND authenticated_at IS NULL AND security_epoch IS NULL AND assurance_level IS NULL AND assurance_expires_at IS NULL AND session_digest IS NULL) OR
        (intent = 'link' AND person_id IS NOT NULL AND authenticated_at IS NOT NULL AND security_epoch >= 0 AND assurance_level IN ('aal1','aal2','aal3') AND assurance_expires_at IS NOT NULL AND session_digest IS NOT NULL AND octet_length(session_digest)=32)
    ),
    CONSTRAINT identity_federation_flows_expiry CHECK (expires_at > created_at)
);

CREATE INDEX identity_federation_flows_pending_idx
    ON identity_federation_flows (expires_at) WHERE consumed_at IS NULL;
