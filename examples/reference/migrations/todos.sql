-- Reference shared-workspace todos. The integrator assigns global ordering.
CREATE TABLE reference_todos (
    id UUID PRIMARY KEY,
    installation_id UUID NOT NULL,
    application_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    creator_person_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT reference_todos_uuid_v7 CHECK (
        substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')
    ),
    CONSTRAINT reference_todos_workspace_fk FOREIGN KEY (workspace_id, installation_id, application_id)
        REFERENCES workspaces (id, installation_id, application_id),
    CONSTRAINT reference_todos_creator_fk FOREIGN KEY (creator_person_id, installation_id, application_id)
        REFERENCES identity_persons (id, installation_id, application_id)
);

CREATE INDEX reference_todos_workspace_created_idx
    ON reference_todos (installation_id, application_id, workspace_id, created_at, id);
