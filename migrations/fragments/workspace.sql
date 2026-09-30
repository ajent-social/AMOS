-- Workspace and membership persistence proposal. The integrator assigns the
-- global migration sequence. Every external identifier is UUIDv7.

CREATE TABLE workspace_role_versions (
    role_key TEXT NOT NULL CHECK (role_key IN ('owner', 'admin', 'member')),
    version INTEGER NOT NULL CHECK (version > 0),
    permissions TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (role_key, version)
);

INSERT INTO workspace_role_versions (role_key, version, permissions) VALUES
    ('owner', 1, ARRAY['workspace.members.read','workspace.members.manage','workspace.owners.manage','workspace.authentication.manage','workspace.delete','billing.manage','billing.read']),
    ('admin', 1, ARRAY['workspace.members.read','workspace.members.manage','billing.read']),
    ('member', 1, ARRAY['workspace.members.read']);

CREATE FUNCTION workspace_role_versions_are_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'workspace role versions are immutable; insert a new version' USING ERRCODE = 'integrity_constraint_violation';
    RETURN NULL;
END $$;
CREATE TRIGGER workspace_role_versions_immutable
    BEFORE UPDATE OR DELETE ON workspace_role_versions
    FOR EACH ROW EXECUTE FUNCTION workspace_role_versions_are_immutable();

CREATE TABLE workspaces (
    id UUID PRIMARY KEY,
    installation_id UUID NOT NULL,
    application_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('personal', 'organization')),
    state TEXT NOT NULL CHECK (state IN ('active', 'suspended', 'deletion_pending')),
    personal_owner_id UUID,
    workspace_epoch BIGINT NOT NULL DEFAULT 0 CHECK (workspace_epoch >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT workspaces_uuid_v7 CHECK (
        substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT workspaces_scope_key UNIQUE (id, installation_id, application_id),
    CONSTRAINT workspaces_personal_owner_shape CHECK (
        (kind = 'personal' AND personal_owner_id IS NOT NULL) OR
        (kind = 'organization' AND personal_owner_id IS NULL)
    ),
    CONSTRAINT workspaces_personal_owner_fk FOREIGN KEY (personal_owner_id, installation_id, application_id)
        REFERENCES identity_persons (id, installation_id, application_id)
);

CREATE UNIQUE INDEX workspaces_personal_owner_unique
    ON workspaces (installation_id, application_id, personal_owner_id)
    WHERE kind = 'personal';
CREATE INDEX workspaces_scope_kind_state_idx
    ON workspaces (installation_id, application_id, kind, state);

CREATE TABLE workspace_memberships (
    id UUID PRIMARY KEY,
    installation_id UUID NOT NULL,
    application_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    person_id UUID NOT NULL,
    role_key TEXT NOT NULL,
    role_version INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'suspended', 'left')),
    membership_epoch BIGINT NOT NULL DEFAULT 0 CHECK (membership_epoch >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT workspace_memberships_uuid_v7 CHECK (
        substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT workspace_memberships_role_fk FOREIGN KEY (role_key, role_version)
        REFERENCES workspace_role_versions (role_key, version),
    CONSTRAINT workspace_memberships_scope_person_key
        UNIQUE (installation_id, application_id, workspace_id, person_id),
    CONSTRAINT workspace_memberships_workspace_fk FOREIGN KEY (workspace_id, installation_id, application_id)
        REFERENCES workspaces (id, installation_id, application_id),
    CONSTRAINT workspace_memberships_person_fk FOREIGN KEY (person_id, installation_id, application_id)
        REFERENCES identity_persons (id, installation_id, application_id)
);

CREATE INDEX workspace_memberships_person_state_idx
    ON workspace_memberships (installation_id, application_id, person_id, state, membership_epoch);
CREATE INDEX workspace_memberships_workspace_role_idx
    ON workspace_memberships (installation_id, application_id, workspace_id, role_key, state);

CREATE FUNCTION workspace_membership_requires_organization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_workspace UUID;
DECLARE target_kind TEXT;
DECLARE target_person UUID;
DECLARE target_state TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_workspace := OLD.workspace_id;
        target_person := OLD.person_id;
        target_state := OLD.state;
    ELSE
        target_workspace := NEW.workspace_id;
        target_person := NEW.person_id;
        target_state := NEW.state;
    END IF;
    SELECT kind INTO target_kind FROM workspaces WHERE id = target_workspace;
    IF target_kind IS NOT NULL AND target_kind <> 'organization' THEN
        RAISE EXCEPTION 'memberships are only valid for organization workspaces' USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP <> 'DELETE' AND target_state = 'active' AND NOT EXISTS (
        SELECT 1 FROM identity_persons p WHERE p.id = target_person AND p.state = 'active'
    ) THEN
        RAISE EXCEPTION 'active membership requires an active person' USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER workspace_membership_kind_guard
    BEFORE INSERT OR UPDATE OR DELETE ON workspace_memberships
    FOR EACH ROW EXECUTE FUNCTION workspace_membership_requires_organization();

CREATE FUNCTION workspace_assert_active_organization_owner(target_workspace UUID) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE target_kind TEXT;
DECLARE target_state TEXT;
BEGIN
    -- Serialize every owner-invariant evaluation, including concurrent account
    -- disables where each transaction otherwise still sees the other's owner.
    SELECT kind, state INTO target_kind, target_state FROM workspaces WHERE id = target_workspace FOR UPDATE;
    IF target_kind = 'organization' AND target_state = 'active' AND NOT EXISTS (
        SELECT 1 FROM workspace_memberships m JOIN identity_persons p ON p.id = m.person_id
        WHERE m.workspace_id = target_workspace AND m.role_key = 'owner' AND m.state = 'active' AND p.state = 'active'
    ) THEN
        RAISE EXCEPTION 'active organization requires an active owner' USING ERRCODE = 'check_violation';
    END IF;
END $$;

CREATE FUNCTION workspace_active_organization_requires_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_workspace UUID;
BEGIN
    IF TG_TABLE_NAME = 'workspaces' THEN
        IF TG_OP = 'DELETE' THEN target_workspace := OLD.id; ELSE target_workspace := NEW.id; END IF;
        PERFORM workspace_assert_active_organization_owner(target_workspace);
    ELSE
        IF TG_OP = 'DELETE' THEN
            PERFORM workspace_assert_active_organization_owner(OLD.workspace_id);
        ELSE
            PERFORM workspace_assert_active_organization_owner(NEW.workspace_id);
            IF TG_OP = 'UPDATE' AND OLD.workspace_id <> NEW.workspace_id THEN
                PERFORM workspace_assert_active_organization_owner(OLD.workspace_id);
            END IF;
        END IF;
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER workspace_active_org_owner_workspace
    AFTER INSERT OR UPDATE ON workspaces
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    EXECUTE FUNCTION workspace_active_organization_requires_owner();
CREATE CONSTRAINT TRIGGER workspace_active_org_owner_membership
    AFTER INSERT OR UPDATE OR DELETE ON workspace_memberships
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    EXECUTE FUNCTION workspace_active_organization_requires_owner();

CREATE FUNCTION workspace_person_state_preserves_active_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_workspace UUID;
BEGIN
    FOR target_workspace IN
        SELECT m.workspace_id FROM workspace_memberships m
        JOIN workspaces w ON w.id = m.workspace_id
        WHERE m.person_id = NEW.id AND m.role_key = 'owner' AND m.state = 'active'
          AND w.kind = 'organization' AND w.state = 'active'
    LOOP
        PERFORM workspace_assert_active_organization_owner(target_workspace);
    END LOOP;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER workspace_person_state_owner_guard
    AFTER UPDATE OF state ON identity_persons
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    EXECUTE FUNCTION workspace_person_state_preserves_active_owner();
