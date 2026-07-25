CREATE TYPE issue_tracker_provider AS ENUM ('jira', 'linear');
CREATE TYPE issue_tracker_link_status AS ENUM ('pending', 'created', 'failed');

GRANT USAGE ON TYPE issue_tracker_provider TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE issue_tracker_link_status TO fidibeki_app, fidibeki_admin;

-- One row per workspace and provider. Credentials are stored as AES-256-GCM ciphertext under
-- INTEGRATION_ENCRYPTION_KEY (the same cipher the integrations table uses) and are never returned
-- to a client; the fingerprint exists so an operator can tell two credentials apart in the UI.
CREATE TABLE issue_tracker_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    provider issue_tracker_provider NOT NULL,
    site_url text NOT NULL DEFAULT '' CHECK (char_length(site_url) <= 2048),
    account_label text NOT NULL DEFAULT '' CHECK (char_length(account_label) <= 200),
    credential_ciphertext bytea NOT NULL,
    credential_fingerprint text NOT NULL CHECK (char_length(credential_fingerprint) BETWEEN 8 AND 80),
    destination_id text NOT NULL DEFAULT '' CHECK (char_length(destination_id) <= 200),
    destination_key text NOT NULL DEFAULT '' CHECK (char_length(destination_key) <= 200),
    destination_name text NOT NULL DEFAULT '' CHECK (char_length(destination_name) <= 200),
    issue_type_id text NOT NULL DEFAULT '' CHECK (char_length(issue_type_id) <= 200),
    issue_type_name text NOT NULL DEFAULT '' CHECK (char_length(issue_type_name) <= 200),
    sub_destination_id text NOT NULL DEFAULT '' CHECK (char_length(sub_destination_id) <= 200),
    sub_destination_name text NOT NULL DEFAULT '' CHECK (char_length(sub_destination_name) <= 200),
    is_enabled boolean NOT NULL DEFAULT false,
    validated_at timestamptz,
    last_error text NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_tracker_connections_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT issue_tracker_connections_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT issue_tracker_connections_tenant_provider_key UNIQUE (tenant_id, provider),
    -- A provider can only be switched on once a live validation resolved a destination, so
    -- "enabled" can never mean "configured but unusable".
    CONSTRAINT issue_tracker_connections_enabled_requires_destination CHECK (
        NOT is_enabled OR (destination_id <> '' AND validated_at IS NOT NULL)
    ),
    -- Jira creates into a project plus an issue type; Linear creates into a team.
    CONSTRAINT issue_tracker_connections_jira_issue_type CHECK (
        provider <> 'jira' OR NOT is_enabled OR issue_type_id <> ''
    ),
    CONSTRAINT issue_tracker_connections_jira_site CHECK (
        provider <> 'jira' OR site_url <> ''
    ),
    CONSTRAINT issue_tracker_connections_linear_site CHECK (
        provider <> 'linear' OR site_url = ''
    )
);

CREATE INDEX idx_issue_tracker_connections_enabled
    ON issue_tracker_connections (tenant_id, provider) WHERE is_enabled;

-- One row per external creation attempt. The Fidibeki issue stays the source record; this table
-- only remembers where a copy was created, whether the attempt succeeded, and why it failed.
CREATE TABLE issue_external_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    provider issue_tracker_provider NOT NULL,
    status issue_tracker_link_status NOT NULL DEFAULT 'pending',
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 200),
    request_title text NOT NULL CHECK (char_length(request_title) BETWEEN 1 AND 255),
    request_description text NOT NULL DEFAULT '' CHECK (char_length(request_description) <= 10000),
    request_priority text NOT NULL DEFAULT ''
        CHECK (request_priority IN ('', 'none', 'low', 'medium', 'high', 'urgent')),
    destination_id text NOT NULL CHECK (char_length(destination_id) BETWEEN 1 AND 200),
    destination_name text NOT NULL DEFAULT '' CHECK (char_length(destination_name) <= 200),
    issue_type_id text NOT NULL DEFAULT '' CHECK (char_length(issue_type_id) <= 200),
    external_id text NOT NULL DEFAULT '' CHECK (char_length(external_id) <= 200),
    external_key text NOT NULL DEFAULT '' CHECK (char_length(external_key) <= 200),
    external_url text NOT NULL DEFAULT '' CHECK (char_length(external_url) <= 2048),
    last_error text NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 2000),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    created_by_member_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_external_links_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT issue_external_links_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_external_links_tenant_member_fkey FOREIGN KEY (tenant_id, created_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (created_by_member_id),
    CONSTRAINT issue_external_links_idempotency_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT issue_external_links_created_requires_identity CHECK (
        status <> 'created' OR (external_id <> '' AND external_url <> '')
    ),
    CONSTRAINT issue_external_links_failed_requires_error CHECK (
        status <> 'failed' OR last_error <> ''
    )
);

-- A retry, a double-submitted form, or a second member acting on the same issue cannot create a
-- second external issue while one is already pending or created. Only failed attempts are
-- excluded, because those are the ones a retry is allowed to replace.
CREATE UNIQUE INDEX idx_issue_external_links_live
    ON issue_external_links (tenant_id, issue_id, provider) WHERE status <> 'failed';
CREATE INDEX idx_issue_external_links_issue
    ON issue_external_links (tenant_id, issue_id, created_at DESC);

ALTER TABLE issue_tracker_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_tracker_connections FORCE ROW LEVEL SECURITY;
ALTER TABLE issue_external_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_external_links FORCE ROW LEVEL SECURITY;

CREATE POLICY issue_tracker_connections_tenant_isolation ON issue_tracker_connections
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY issue_external_links_tenant_isolation ON issue_external_links
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON issue_tracker_connections TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON issue_external_links TO fidibeki_app, fidibeki_admin;
