ALTER TYPE issue_status ADD VALUE IF NOT EXISTS 'needs_info' AFTER 'in_progress';

ALTER TABLE tenants
    ADD COLUMN reporter_access_enabled boolean NOT NULL DEFAULT true;

ALTER TABLE projects
    ADD COLUMN reporter_access_enabled boolean;

ALTER TABLE issues
    ADD COLUMN reporter_access_revoked_at timestamptz;

CREATE TABLE reporter_access_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reporter_access_tokens_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT reporter_access_tokens_project_issue_fkey FOREIGN KEY (project_id, issue_id)
        REFERENCES issues(project_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_reporter_access_tokens_issue
    ON reporter_access_tokens (tenant_id, issue_id);

CREATE TRIGGER reporter_access_tokens_set_updated_at
BEFORE UPDATE ON reporter_access_tokens
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE reporter_clarification_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    requested_activity_id uuid NOT NULL,
    prompt text NOT NULL CHECK (char_length(btrim(prompt)) BETWEEN 1 AND 2000),
    response text CHECK (response IS NULL OR char_length(btrim(response)) BETWEEN 1 AND 5000),
    responded_activity_id uuid,
    responded_at timestamptz,
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reporter_clarifications_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT reporter_clarifications_requested_activity_fkey
        FOREIGN KEY (tenant_id, requested_activity_id)
        REFERENCES issue_activities(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT reporter_clarifications_responded_activity_fkey
        FOREIGN KEY (tenant_id, responded_activity_id)
        REFERENCES issue_activities(tenant_id, id) ON DELETE SET NULL (responded_activity_id),
    CONSTRAINT reporter_clarifications_response_consistent CHECK (
        (response IS NULL AND responded_at IS NULL AND responded_activity_id IS NULL)
        OR (response IS NOT NULL AND responded_at IS NOT NULL AND responded_activity_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX idx_reporter_clarifications_open
    ON reporter_clarification_requests (tenant_id, issue_id)
    WHERE responded_at IS NULL AND closed_at IS NULL;
CREATE INDEX idx_reporter_clarifications_issue_created
    ON reporter_clarification_requests (tenant_id, issue_id, created_at DESC);

ALTER TABLE reporter_access_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE reporter_access_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY reporter_access_tokens_tenant_isolation ON reporter_access_tokens
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE reporter_clarification_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE reporter_clarification_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY reporter_clarifications_tenant_isolation ON reporter_clarification_requests
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON reporter_access_tokens
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON reporter_clarification_requests
    TO fidibeki_app, fidibeki_admin;
