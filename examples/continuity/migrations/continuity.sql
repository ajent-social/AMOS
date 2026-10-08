-- Unallocated application-owned fragment. Integrator/operator applies only after
-- immutable migration-history review. No seeds, roles or installation wiring.

CREATE TABLE public.continuity_properties (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    area text NOT NULL CHECK (char_length(btrim(area)) BETWEEN 1 AND 200),
    owner_label text NOT NULL CHECK (char_length(btrim(owner_label)) BETWEEN 1 AND 200),
    occupant_label text NOT NULL CHECK (char_length(btrim(occupant_label)) BETWEEN 1 AND 200),
    occupancy text NOT NULL CHECK (occupancy IN ('occupied','vacant','unknown')),
    inspection_date date,
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_sources (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    kind text NOT NULL CHECK (kind IN ('correspondence','document')),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 65536),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_cases (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    property_id uuid NOT NULL,
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    status text NOT NULL CHECK (status IN ('awaiting_owner','ready_to_arrange','in_progress','needs_assessment','completed')),
    revision bigint NOT NULL CHECK (revision > 0),
    source_ids jsonb NOT NULL CHECK (jsonb_typeof(source_ids) = 'array' AND jsonb_array_length(source_ids) BETWEEN 1 AND 32 AND octet_length(source_ids::text) <= 2048),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id),
    FOREIGN KEY (installation_id, application_id, environment_id, workspace_id, property_id) REFERENCES public.continuity_properties (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_applications (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    property_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    items jsonb NOT NULL CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) BETWEEN 1 AND 32 AND octet_length(items::text) <= 65536),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id),
    FOREIGN KEY (installation_id, application_id, environment_id, workspace_id, property_id) REFERENCES public.continuity_properties (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_procedures (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 6000),
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_drafts (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    case_id uuid NOT NULL CHECK (substring(case_id::text, 15, 1) = '7' AND substring(case_id::text, 20, 1) IN ('8','9','a','b')),
    case_revision bigint NOT NULL CHECK (case_revision > 0),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 4000),
    source_ids jsonb NOT NULL CHECK (jsonb_typeof(source_ids) = 'array' AND jsonb_array_length(source_ids) BETWEEN 1 AND 32 AND octet_length(source_ids::text) <= 2048),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, case_id),
    FOREIGN KEY (installation_id, application_id, environment_id, workspace_id, case_id) REFERENCES public.continuity_cases (installation_id, application_id, environment_id, workspace_id, id)
);

CREATE TABLE public.continuity_activity (
    installation_id uuid NOT NULL CHECK (substring(installation_id::text, 15, 1) = '7' AND substring(installation_id::text, 20, 1) IN ('8','9','a','b')),
    application_id uuid NOT NULL CHECK (substring(application_id::text, 15, 1) = '7' AND substring(application_id::text, 20, 1) IN ('8','9','a','b')),
    environment_id uuid NOT NULL CHECK (substring(environment_id::text, 15, 1) = '7' AND substring(environment_id::text, 20, 1) IN ('8','9','a','b')),
    workspace_id uuid NOT NULL CHECK (substring(workspace_id::text, 15, 1) = '7' AND substring(workspace_id::text, 20, 1) IN ('8','9','a','b')),
    id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7' AND substring(id::text, 20, 1) IN ('8','9','a','b')),
    actor_id uuid NOT NULL CHECK (actor_id::text ~ '^[0-9a-f-]{14}7[0-9a-f-]{4}[89ab]'),
    resource_id uuid NOT NULL CHECK (resource_id::text ~ '^[0-9a-f-]{14}7[0-9a-f-]{4}[89ab]'),
    action text NOT NULL CHECK (action IN ('case.changed','checklist.changed','procedure.changed','draft.saved')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (installation_id, application_id, environment_id, workspace_id, id)
);

