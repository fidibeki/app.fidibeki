ALTER TABLE widget_rules
    ADD CONSTRAINT widget_rules_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE feedbacks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    widget_rule_id uuid,
    public_id text NOT NULL,
    issue_id uuid,
    client_feedback_id uuid NOT NULL,
    rating smallint,
    text text,
    ai_job_enqueued_at timestamptz,
    reporter_email text,
    notify_reporter boolean NOT NULL DEFAULT false,
    page_url text NOT NULL,
    referrer text,
    user_agent text,
    os text,
    browser text,
    device text,
    viewport_width integer,
    viewport_height integer,
    language text,
    dom_snapshot_html text,
    dom_snapshot_styles text,
    status feedback_status NOT NULL DEFAULT 'open',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feedbacks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT feedbacks_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT feedbacks_project_rule_fkey FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE SET NULL,
    CONSTRAINT feedbacks_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE SET NULL,
    CONSTRAINT feedbacks_project_issue_fkey FOREIGN KEY (project_id, issue_id)
        REFERENCES issues(project_id, id) ON DELETE SET NULL,
    CONSTRAINT feedbacks_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT feedbacks_tenant_public_id_key UNIQUE (tenant_id, public_id),
    CONSTRAINT feedbacks_project_client_feedback_id_key UNIQUE (project_id, client_feedback_id),
    CONSTRAINT feedbacks_public_id_format CHECK (public_id ~ '^fb_[A-Za-z0-9]{8}$'),
    CONSTRAINT feedbacks_rating_range CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),
    CONSTRAINT feedbacks_text_length CHECK (text IS NULL OR char_length(text) <= 10000),
    CONSTRAINT feedbacks_page_url_length CHECK (char_length(page_url) BETWEEN 1 AND 4096),
    CONSTRAINT feedbacks_reporter_email_required CHECK (NOT notify_reporter OR reporter_email IS NOT NULL),
    CONSTRAINT feedbacks_device_valid CHECK (device IS NULL OR device IN ('desktop', 'tablet', 'mobile')),
    CONSTRAINT feedbacks_viewport_valid CHECK (
        (viewport_width IS NULL OR viewport_width BETWEEN 1 AND 100000) AND
        (viewport_height IS NULL OR viewport_height BETWEEN 1 AND 100000)
    )
);

CREATE INDEX idx_feedbacks_tenant_id ON feedbacks (tenant_id);
CREATE INDEX idx_feedbacks_project_id ON feedbacks (project_id);
CREATE INDEX idx_feedbacks_issue_id ON feedbacks (issue_id);
CREATE INDEX idx_feedbacks_status ON feedbacks (tenant_id, status);
CREATE INDEX idx_feedbacks_created_at ON feedbacks (tenant_id, created_at DESC);

CREATE TRIGGER feedbacks_set_updated_at
BEFORE UPDATE ON feedbacks
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE feedbacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedbacks FORCE ROW LEVEL SECURITY;

CREATE POLICY feedbacks_tenant_isolation ON feedbacks
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON feedbacks TO fidibeki_app, fidibeki_admin;
