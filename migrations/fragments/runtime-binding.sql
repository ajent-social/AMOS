-- One independently operated AMOS application environment per runtime database.
CREATE TABLE amos_runtime_binding (
 singleton boolean PRIMARY KEY CHECK (singleton),
 installation_id uuid NOT NULL,
 application_id uuid NOT NULL,
 environment_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK (get_byte(uuid_send(installation_id),6)>>4=7 AND substring(installation_id::text,20,1) IN ('8','9','a','b')),
 CHECK (get_byte(uuid_send(application_id),6)>>4=7 AND substring(application_id::text,20,1) IN ('8','9','a','b')),
 CHECK (get_byte(uuid_send(environment_id),6)>>4=7 AND substring(environment_id::text,20,1) IN ('8','9','a','b'))
);
CREATE FUNCTION amos_runtime_binding_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'runtime binding is immutable'; END;
$$;
CREATE TRIGGER amos_runtime_binding_no_mutation BEFORE UPDATE OR DELETE ON amos_runtime_binding FOR EACH ROW EXECUTE FUNCTION amos_runtime_binding_immutable();
