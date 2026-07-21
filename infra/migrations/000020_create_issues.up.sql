CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    public_id text NOT NULL,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 80),
    status issue_status NOT NULL DEFAULT 'open',
    merged_into_id uuid,
    merged_at timestamptz,
    reporter_email text,
    notify_reporter boolean NOT NULL DEFAULT false,
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issues_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT issues_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issues_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT issues_project_id_id_key UNIQUE (project_id, id),
    CONSTRAINT issues_tenant_public_id_key UNIQUE (tenant_id, public_id),
    CONSTRAINT issues_public_id_format CHECK (public_id ~ '^[A-Z0-9-]+-[0-9]{4}-[0-9]{4,}$'),
    CONSTRAINT issues_reporter_email_required CHECK (NOT notify_reporter OR reporter_email IS NOT NULL),
    CONSTRAINT issues_merge_consistent CHECK (
        (merged_into_id IS NULL AND merged_at IS NULL) OR
        (merged_into_id IS NOT NULL AND merged_at IS NOT NULL AND merged_into_id <> id)
    )
);

ALTER TABLE issues ADD CONSTRAINT issues_tenant_merge_fkey
    FOREIGN KEY (tenant_id, merged_into_id) REFERENCES issues(tenant_id, id) ON DELETE SET NULL;

CREATE INDEX idx_issues_tenant_id ON issues (tenant_id);
CREATE INDEX idx_issues_project_id ON issues (project_id);
CREATE INDEX idx_issues_status ON issues (tenant_id, status);
CREATE INDEX idx_issues_merged_into_id ON issues (merged_into_id) WHERE merged_into_id IS NOT NULL;

CREATE TRIGGER issues_set_updated_at
BEFORE UPDATE ON issues
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE issues ENABLE ROW LEVEL SECURITY;
ALTER TABLE issues FORCE ROW LEVEL SECURITY;

CREATE POLICY issues_tenant_isolation ON issues
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON issues TO fidibeki_app, fidibeki_admin;
