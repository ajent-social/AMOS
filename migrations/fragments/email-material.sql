-- Protected delivery material, separate from generic outbox payloads.
CREATE TABLE email_delivery_material (
 installation_id UUID NOT NULL,
 application_id UUID NOT NULL,
 environment_id UUID NOT NULL,
 material_id UUID NOT NULL CHECK (substring(material_id::text,15,1)='7' AND substring(material_id::text,20,1) IN ('8','9','a','b')),
 key_id TEXT NOT NULL CHECK (length(key_id) BETWEEN 1 AND 64),
 nonce BYTEA NOT NULL CHECK (octet_length(nonce)=12),
 ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 16 AND 4096),
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(installation_id,application_id,environment_id,material_id),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '24 hour')
);
CREATE INDEX email_delivery_material_expiry ON email_delivery_material(expires_at);
