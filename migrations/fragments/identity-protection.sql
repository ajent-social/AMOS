-- Additive authentication budget counters. Keys are deployment-secret HMACs;
-- no raw IP, email or credential material is persisted.
CREATE TABLE identity_auth_limits (
 installation_id UUID NOT NULL,
 application_id UUID NOT NULL,
 environment_id UUID NOT NULL,
 operation TEXT NOT NULL CHECK (operation IN ('signup','signin','verification','recovery','password_change')),
 dimension TEXT NOT NULL CHECK (dimension IN ('ip','account')),
 key_digest BYTEA NOT NULL CHECK (octet_length(key_digest)=32),
 window_start TIMESTAMPTZ NOT NULL,
 window_end TIMESTAMPTZ NOT NULL CHECK (window_end > window_start),
 attempts INTEGER NOT NULL CHECK (attempts > 0),
 PRIMARY KEY (installation_id,application_id,environment_id,operation,dimension,key_digest)
);
CREATE INDEX identity_auth_limits_expiry ON identity_auth_limits(window_end);
