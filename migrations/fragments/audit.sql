-- Security audit events are immutable, tenant-scoped records. The integrator
-- assigns this fragment's single global migration sequence in the registry.
CREATE TABLE amos_security_audit_events (
    id uuid PRIMARY KEY CHECK (get_byte(uuid_send(id), 6) >> 4 = 7),
    installation_id uuid NOT NULL CHECK (get_byte(uuid_send(installation_id), 6) >> 4 = 7),
    application_id uuid NOT NULL CHECK (get_byte(uuid_send(application_id), 6) >> 4 = 7),
    environment_id uuid NOT NULL CHECK (get_byte(uuid_send(environment_id), 6) >> 4 = 7),
    workspace_id uuid NOT NULL CHECK (get_byte(uuid_send(workspace_id), 6) >> 4 = 7),
    actor_kind text NOT NULL CHECK (actor_kind IN ('person', 'machine')),
    actor_id uuid NOT NULL CHECK (get_byte(uuid_send(actor_id), 6) >> 4 = 7),
    action text NOT NULL CHECK (action IN (
        'material.created', 'material.updated', 'material.revoked',
        'session.issued', 'session.revoked', 'workspace.membership_changed',
        'policy.grant_changed', 'security.access_denied'
    )),
    resource_type text NOT NULL CHECK (resource_type IN ('material', 'session', 'workspace', 'grant', 'credential')),
    resource_id uuid NOT NULL CHECK (get_byte(uuid_send(resource_id), 6) >> 4 = 7),
    outcome text NOT NULL CHECK (outcome IN ('succeeded', 'denied', 'unavailable')),
    correlation_id uuid NOT NULL CHECK (get_byte(uuid_send(correlation_id), 6) >> 4 = 7),
    attributes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(attributes) = 'array' AND jsonb_array_length(attributes) <= 4),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE INDEX amos_security_audit_scope_page_idx
    ON amos_security_audit_events (installation_id, application_id, environment_id, workspace_id, created_at DESC, id DESC);

CREATE FUNCTION amos_security_audit_guard()
RETURNS trigger LANGUAGE plpgsql AS $function$
DECLARE
    item jsonb;
    item_key text;
    item_value text;
    seen_keys text[] := ARRAY[]::text[];
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'security audit events are append only';
    END IF;
    IF jsonb_typeof(NEW.attributes) <> 'array' OR jsonb_array_length(NEW.attributes) > 4 THEN
        RAISE EXCEPTION 'invalid security audit attributes';
    END IF;
    FOR item IN SELECT value FROM jsonb_array_elements(NEW.attributes) AS entry(value) LOOP
        IF jsonb_typeof(item) <> 'object' THEN
            RAISE EXCEPTION 'invalid security audit attribute';
        END IF;
        IF (SELECT count(*) FROM jsonb_object_keys(item)) <> 2 OR NOT (item ? 'key') OR NOT (item ? 'value') THEN
            RAISE EXCEPTION 'invalid security audit attribute';
        END IF;
        item_key := item ->> 'key';
        item_value := item ->> 'value';
        IF item_key IS NULL OR item_value IS NULL OR item_key = ANY(seen_keys) THEN
            RAISE EXCEPTION 'invalid security audit attribute';
        END IF;
        IF NOT (
            (item_key = 'factor' AND item_value IN ('password', 'webauthn', 'totp', 'api_key')) OR
            (item_key = 'category' AND item_value IN ('identity', 'workspace', 'billing', 'secret')) OR
            (item_key = 'state' AND item_value IN ('created', 'updated', 'revoked', 'enabled', 'disabled', 'denied'))
        ) THEN
            RAISE EXCEPTION 'invalid security audit attribute';
        END IF;
        seen_keys := array_append(seen_keys, item_key);
    END LOOP;
    NEW.created_at := transaction_timestamp();
    RETURN NEW;
END;
$function$;

CREATE TRIGGER amos_security_audit_row_guard
    BEFORE INSERT OR UPDATE OR DELETE ON amos_security_audit_events
    FOR EACH ROW EXECUTE FUNCTION amos_security_audit_guard();

CREATE TRIGGER amos_security_audit_truncate_guard
    BEFORE TRUNCATE ON amos_security_audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION amos_security_audit_guard();
