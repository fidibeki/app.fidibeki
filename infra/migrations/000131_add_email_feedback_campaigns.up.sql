CREATE TABLE email_feedback_campaigns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    widget_rule_id uuid NOT NULL,
    campaign_key text NOT NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    default_locale text NOT NULL,
    subject text NOT NULL,
    introduction text NOT NULL DEFAULT '',
    invitation_ttl_days smallint NOT NULL DEFAULT 30,
    created_by uuid,
    updated_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT email_feedback_campaigns_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT email_feedback_campaigns_project_rule_fkey
        FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT email_feedback_campaigns_tenant_created_by_fkey
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (created_by),
    CONSTRAINT email_feedback_campaigns_tenant_updated_by_fkey
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (updated_by),
    CONSTRAINT email_feedback_campaigns_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT email_feedback_campaigns_tenant_key_key UNIQUE (tenant_id, campaign_key),
    CONSTRAINT email_feedback_campaigns_key_format CHECK (
        campaign_key ~ '^[a-z][a-z0-9-]{2,63}$'
    ),
    CONSTRAINT email_feedback_campaigns_name_length CHECK (
        char_length(name) BETWEEN 1 AND 120
    ),
    CONSTRAINT email_feedback_campaigns_status_valid CHECK (
        status IN ('draft', 'active', 'paused', 'archived')
    ),
    CONSTRAINT email_feedback_campaigns_locale_format CHECK (
        char_length(default_locale) BETWEEN 2 AND 35
        AND default_locale ~ '^[a-z]{2,3}(-[a-z0-9]{2,8})*$'
    ),
    CONSTRAINT email_feedback_campaigns_subject_length CHECK (
        char_length(subject) BETWEEN 1 AND 160
    ),
    CONSTRAINT email_feedback_campaigns_introduction_length CHECK (
        char_length(introduction) <= 1000
    ),
    CONSTRAINT email_feedback_campaigns_ttl_range CHECK (
        invitation_ttl_days BETWEEN 1 AND 90
    )
);

CREATE INDEX idx_email_feedback_campaigns_project
    ON email_feedback_campaigns (tenant_id, project_id, created_at, id);
CREATE INDEX idx_email_feedback_campaigns_active
    ON email_feedback_campaigns (tenant_id, id)
    WHERE status = 'active';

CREATE TRIGGER email_feedback_campaigns_set_updated_at
BEFORE UPDATE ON email_feedback_campaigns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE email_feedback_campaigns ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_feedback_campaigns FORCE ROW LEVEL SECURITY;

CREATE POLICY email_feedback_campaigns_tenant_isolation ON email_feedback_campaigns
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE
    ON email_feedback_campaigns TO fidibeki_app, fidibeki_admin;
