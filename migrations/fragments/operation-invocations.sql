-- Additive operation storage. Existing migration bytes and audit guards stay
-- unchanged. Capacity selectors and durable results are never authority.
CREATE FUNCTION amos_operation_uuid7(value uuid) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $function$
    SELECT get_byte(uuid_send(value), 6) >> 4 = 7
       AND substring(value::text, 20, 1) IN ('8', '9', 'a', 'b');
$function$;

CREATE TABLE amos_operation_installation_capacity (
    installation_id uuid PRIMARY KEY CHECK (amos_operation_uuid7(installation_id)),
    hard_bytes bigint NOT NULL CHECK (hard_bytes > 1),
    allocated_bytes bigint NOT NULL DEFAULT 0 CHECK (allocated_bytes >= 0 AND allocated_bytes <= hard_bytes)
);
CREATE TABLE amos_operation_actor_capacity (
    installation_id uuid NOT NULL REFERENCES amos_operation_installation_capacity(installation_id),
    application_id uuid NOT NULL CHECK (amos_operation_uuid7(application_id)),
    environment_id uuid NOT NULL CHECK (amos_operation_uuid7(environment_id)),
    actor_kind text NOT NULL CHECK (actor_kind = 'person'),
    actor_id uuid NOT NULL CHECK (amos_operation_uuid7(actor_id)),
    limit_bytes bigint NOT NULL CHECK (limit_bytes > 0),
    used_bytes bigint NOT NULL DEFAULT 0 CHECK (used_bytes >= 0 AND used_bytes <= limit_bytes),
    PRIMARY KEY (installation_id, application_id, environment_id, actor_kind, actor_id)
);
CREATE TABLE amos_operation_workspace_capacity (
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    actor_kind text NOT NULL,
    actor_id uuid NOT NULL,
    workspace_id uuid NOT NULL CHECK (amos_operation_uuid7(workspace_id)),
    limit_bytes bigint NOT NULL CHECK (limit_bytes > 0),
    used_bytes bigint NOT NULL DEFAULT 0 CHECK (used_bytes >= 0 AND used_bytes <= limit_bytes),
    PRIMARY KEY (installation_id, application_id, environment_id, actor_kind, actor_id, workspace_id),
    FOREIGN KEY (installation_id, application_id, environment_id, actor_kind, actor_id)
        REFERENCES amos_operation_actor_capacity(installation_id, application_id, environment_id, actor_kind, actor_id)
);
CREATE TABLE amos_operation_invocations (
    id uuid PRIMARY KEY CHECK (amos_operation_uuid7(id)),
    installation_id uuid NOT NULL,
    application_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    actor_kind text NOT NULL,
    actor_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    operation_id text NOT NULL CHECK (octet_length(operation_id) BETWEEN 3 AND 120
        AND operation_id COLLATE "C" ~ '^[a-z][a-z0-9]*([.-][a-z0-9]+)*$'),
    key_digest bytea NOT NULL CHECK (octet_length(key_digest) = 32),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    operation_revision text NOT NULL CHECK (octet_length(operation_revision) BETWEEN 1 AND 160
        AND operation_revision = btrim(operation_revision)
        AND operation_revision !~ '[[:cntrl:]]'),
    descriptor_digest bytea NOT NULL CHECK (octet_length(descriptor_digest) = 32),
    input_schema_digest bytea NOT NULL CHECK (octet_length(input_schema_digest) = 32),
    output_schema_digest bytea NOT NULL CHECK (octet_length(output_schema_digest) = 32),
    reserved_bytes bigint NOT NULL CHECK (reserved_bytes BETWEEN 1 AND 65536),
    state text NOT NULL CHECK (state IN ('pending', 'completed')),
    result_kind text,
    result_json bytea,
    result_sha256 bytea,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    UNIQUE (installation_id, application_id, environment_id, actor_kind, actor_id, workspace_id, operation_id, key_digest),
    FOREIGN KEY (installation_id, application_id, environment_id, actor_kind, actor_id, workspace_id)
        REFERENCES amos_operation_workspace_capacity(installation_id, application_id, environment_id, actor_kind, actor_id, workspace_id),
    CHECK (
        (state = 'pending' AND result_kind IS NULL AND result_json IS NULL AND result_sha256 IS NULL AND completed_at IS NULL)
        OR
        (state = 'completed' AND result_kind IS NOT NULL AND result_kind IN ('succeeded', 'created', 'accepted', 'no_content')
         AND result_json IS NOT NULL AND octet_length(result_json) BETWEEN 1 AND 65536
         AND reserved_bytes = octet_length(result_json)
         AND convert_from(result_json, 'UTF8')::json IS NOT NULL
         AND result_sha256 IS NOT NULL AND octet_length(result_sha256) = 32
         AND completed_at IS NOT NULL)
    )
);

CREATE FUNCTION amos_operation_invocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $function$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'pending' THEN RAISE EXCEPTION 'operation invocation must begin pending'; END IF;
        NEW.created_at := transaction_timestamp();
        RETURN NEW;
    END IF;
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'operation invocation is immutable';
    END IF;
    IF OLD.state <> 'pending' OR NEW.state <> 'completed' THEN
        RAISE EXCEPTION 'operation invocation is immutable';
    END IF;
    IF ROW(NEW.id, NEW.installation_id, NEW.application_id, NEW.environment_id,
           NEW.actor_kind, NEW.actor_id, NEW.workspace_id, NEW.operation_id,
           NEW.key_digest, NEW.request_hash, NEW.operation_revision,
           NEW.descriptor_digest, NEW.input_schema_digest, NEW.output_schema_digest, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.installation_id, OLD.application_id, OLD.environment_id,
           OLD.actor_kind, OLD.actor_id, OLD.workspace_id, OLD.operation_id,
           OLD.key_digest, OLD.request_hash, OLD.operation_revision,
           OLD.descriptor_digest, OLD.input_schema_digest, OLD.output_schema_digest, OLD.created_at)
       OR NEW.reserved_bytes > OLD.reserved_bytes THEN
        RAISE EXCEPTION 'operation invocation binding is immutable';
    END IF;
    NEW.completed_at := transaction_timestamp();
    RETURN NEW;
END;
$function$;
CREATE TRIGGER amos_operation_invocation_guard
    BEFORE INSERT OR UPDATE OR DELETE ON amos_operation_invocations
    FOR EACH ROW EXECUTE FUNCTION amos_operation_invocation_guard();
CREATE TRIGGER amos_operation_invocation_truncate_guard
    BEFORE TRUNCATE ON amos_operation_invocations
    FOR EACH STATEMENT EXECUTE FUNCTION amos_operation_invocation_guard();

CREATE FUNCTION amos_operation_require_completion() RETURNS trigger
LANGUAGE plpgsql AS $function$
DECLARE current_state text;
BEGIN
    -- Resolve the actual table schema, not a caller's search_path or temp table.
    EXECUTE format('SELECT state FROM %I.amos_operation_invocations WHERE id=$1', TG_TABLE_SCHEMA)
        INTO current_state USING NEW.id;
    IF current_state IS DISTINCT FROM 'completed' THEN
        RAISE EXCEPTION 'operation invocation is incomplete';
    END IF;
    RETURN NULL;
END;
$function$;
CREATE CONSTRAINT TRIGGER amos_operation_completion_required
    AFTER INSERT OR UPDATE ON amos_operation_invocations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    EXECUTE FUNCTION amos_operation_require_completion();

-- Freeze schema-qualified references into owner functions when migrating.
-- They are SECURITY INVOKER and never available to the ordinary runtime role.
-- Row triggers cannot establish the installation-before-actor allocation order.
DO $migration$
DECLARE schema_name text := quote_ident(current_schema());
BEGIN
    EXECUTE replace($definition$
CREATE FUNCTION __schema__.amos_operation_install_capacity(p_installation uuid, p_limit bigint)
RETURNS void LANGUAGE plpgsql SECURITY INVOKER AS $body$
DECLARE old_limit bigint;
BEGIN
    IF p_installation IS NULL OR NOT __schema__.amos_operation_uuid7(p_installation)
       OR p_limit IS NULL OR p_limit <= 1 THEN RAISE EXCEPTION 'invalid operation capacity'; END IF;
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'invalid operation capacity transaction';
    END IF;
    INSERT INTO __schema__.amos_operation_installation_capacity(installation_id, hard_bytes)
        VALUES(p_installation, p_limit) ON CONFLICT DO NOTHING;
    SELECT hard_bytes INTO STRICT old_limit FROM __schema__.amos_operation_installation_capacity
        WHERE installation_id=p_installation FOR UPDATE;
    IF p_limit < old_limit THEN RAISE EXCEPTION 'operation capacity decrease is not supported'; END IF;
    UPDATE __schema__.amos_operation_installation_capacity SET hard_bytes=p_limit
        WHERE installation_id=p_installation;
END;
$body$;
$definition$, '__schema__', schema_name);

    EXECUTE replace($definition$
CREATE FUNCTION __schema__.amos_operation_allocate_actor(p_installation uuid, p_application uuid,
    p_environment uuid, p_kind text, p_actor uuid, p_limit bigint)
RETURNS void LANGUAGE plpgsql SECURITY INVOKER AS $body$
DECLARE hard bigint; allocated bigint; old_limit bigint; change_bytes bigint; value uuid;
BEGIN
    FOREACH value IN ARRAY ARRAY[p_installation,p_application,p_environment,p_actor] LOOP
        IF value IS NULL OR NOT __schema__.amos_operation_uuid7(value) THEN RAISE EXCEPTION 'invalid operation capacity'; END IF;
    END LOOP;
    IF p_kind IS DISTINCT FROM 'person' OR p_limit IS NULL OR p_limit <= 0 THEN RAISE EXCEPTION 'invalid operation capacity'; END IF;
    IF current_setting('transaction_isolation') <> 'read committed' THEN RAISE EXCEPTION 'invalid operation capacity transaction'; END IF;
    SELECT hard_bytes,allocated_bytes INTO hard,allocated FROM __schema__.amos_operation_installation_capacity
        WHERE installation_id=p_installation FOR UPDATE;
    IF NOT FOUND OR p_limit >= hard THEN RAISE EXCEPTION 'operation capacity unavailable'; END IF;
    SELECT limit_bytes INTO old_limit FROM __schema__.amos_operation_actor_capacity
        WHERE installation_id=p_installation AND application_id=p_application AND environment_id=p_environment
          AND actor_kind=p_kind AND actor_id=p_actor FOR UPDATE;
    old_limit := coalesce(old_limit,0);
    IF p_limit < old_limit THEN RAISE EXCEPTION 'operation capacity decrease is not supported'; END IF;
    change_bytes := p_limit-old_limit;
    IF change_bytes > hard-allocated THEN RAISE EXCEPTION 'operation capacity unavailable'; END IF;
    IF old_limit = 0 THEN
        INSERT INTO __schema__.amos_operation_actor_capacity
            (installation_id,application_id,environment_id,actor_kind,actor_id,limit_bytes)
            VALUES(p_installation,p_application,p_environment,p_kind,p_actor,p_limit);
    ELSE
        UPDATE __schema__.amos_operation_actor_capacity SET limit_bytes=p_limit
            WHERE installation_id=p_installation AND application_id=p_application AND environment_id=p_environment
              AND actor_kind=p_kind AND actor_id=p_actor;
    END IF;
    UPDATE __schema__.amos_operation_installation_capacity SET allocated_bytes=allocated_bytes+change_bytes
        WHERE installation_id=p_installation;
END;
$body$;
$definition$, '__schema__', schema_name);

    EXECUTE replace($definition$
CREATE FUNCTION __schema__.amos_operation_allocate_workspace(p_installation uuid,p_application uuid,
    p_environment uuid,p_kind text,p_actor uuid,p_workspace uuid,p_limit bigint)
RETURNS void LANGUAGE plpgsql SECURITY INVOKER AS $body$
DECLARE hard bigint; actor_limit bigint; old_limit bigint; value uuid;
BEGIN
    FOREACH value IN ARRAY ARRAY[p_installation,p_application,p_environment,p_actor,p_workspace] LOOP
        IF value IS NULL OR NOT __schema__.amos_operation_uuid7(value) THEN RAISE EXCEPTION 'invalid operation capacity'; END IF;
    END LOOP;
    IF p_kind IS DISTINCT FROM 'person' OR p_limit IS NULL OR p_limit <= 0 THEN RAISE EXCEPTION 'invalid operation capacity'; END IF;
    IF current_setting('transaction_isolation') <> 'read committed' THEN RAISE EXCEPTION 'invalid operation capacity transaction'; END IF;
    SELECT hard_bytes INTO hard FROM __schema__.amos_operation_installation_capacity
        WHERE installation_id=p_installation FOR UPDATE;
    IF NOT FOUND OR p_limit >= hard THEN RAISE EXCEPTION 'operation capacity unavailable'; END IF;
    SELECT limit_bytes INTO actor_limit FROM __schema__.amos_operation_actor_capacity
        WHERE installation_id=p_installation AND application_id=p_application AND environment_id=p_environment
          AND actor_kind=p_kind AND actor_id=p_actor FOR UPDATE;
    IF NOT FOUND OR p_limit > actor_limit THEN RAISE EXCEPTION 'operation capacity unavailable'; END IF;
    SELECT limit_bytes INTO old_limit FROM __schema__.amos_operation_workspace_capacity
        WHERE installation_id=p_installation AND application_id=p_application AND environment_id=p_environment
          AND actor_kind=p_kind AND actor_id=p_actor AND workspace_id=p_workspace FOR UPDATE;
    IF FOUND THEN
        IF p_limit < old_limit THEN RAISE EXCEPTION 'operation capacity decrease is not supported'; END IF;
        UPDATE __schema__.amos_operation_workspace_capacity SET limit_bytes=p_limit
            WHERE installation_id=p_installation AND application_id=p_application AND environment_id=p_environment
              AND actor_kind=p_kind AND actor_id=p_actor AND workspace_id=p_workspace;
    ELSE
        INSERT INTO __schema__.amos_operation_workspace_capacity
            (installation_id,application_id,environment_id,actor_kind,actor_id,workspace_id,limit_bytes)
            VALUES(p_installation,p_application,p_environment,p_kind,p_actor,p_workspace,p_limit);
    END IF;
END;
$body$;
$definition$, '__schema__', schema_name);
END;
$migration$;
REVOKE ALL ON FUNCTION amos_operation_install_capacity(uuid,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION amos_operation_allocate_actor(uuid,uuid,uuid,text,uuid,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION amos_operation_allocate_workspace(uuid,uuid,uuid,text,uuid,uuid,bigint) FROM PUBLIC;

ALTER TABLE amos_security_audit_events ADD COLUMN operation_id text;
ALTER TABLE amos_security_audit_events DROP CONSTRAINT amos_security_audit_events_action_check;
ALTER TABLE amos_security_audit_events DROP CONSTRAINT amos_security_audit_events_resource_type_check;
ALTER TABLE amos_security_audit_events ADD CONSTRAINT amos_security_audit_events_action_check
    CHECK (action IN ('material.created','material.updated','material.revoked','session.issued','session.revoked',
        'workspace.membership_changed','policy.grant_changed','security.access_denied','operation.invoked'));
ALTER TABLE amos_security_audit_events ADD CONSTRAINT amos_security_audit_events_resource_type_check
    CHECK (resource_type IN ('material','session','workspace','grant','credential','invocation'));
ALTER TABLE amos_security_audit_events ADD CONSTRAINT amos_security_audit_invocation_binding_check
    CHECK (
        (action='operation.invoked' AND resource_type='invocation' AND operation_id IS NOT NULL
         AND octet_length(operation_id) BETWEEN 3 AND 120
         AND operation_id COLLATE "C" ~ '^[a-z][a-z0-9]*([.-][a-z0-9]+)*$' AND attributes='[]'::jsonb)
        OR (action<>'operation.invoked' AND resource_type<>'invocation' AND operation_id IS NULL)
    );
