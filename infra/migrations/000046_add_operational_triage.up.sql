ALTER TABLE feedbacks
    ADD COLUMN priority text,
    ADD COLUMN assignee_id uuid,
    ADD CONSTRAINT feedbacks_priority_valid CHECK (
        priority IS NULL OR priority IN ('low', 'medium', 'high', 'urgent')
    ),
    ADD CONSTRAINT feedbacks_tenant_assignee_fkey
        FOREIGN KEY (tenant_id, assignee_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE SET NULL (assignee_id);

ALTER TABLE issues
    ADD COLUMN priority text,
    ADD COLUMN assignee_id uuid,
    ADD CONSTRAINT issues_priority_valid CHECK (
        priority IS NULL OR priority IN ('low', 'medium', 'high', 'urgent')
    ),
    ADD CONSTRAINT issues_tenant_assignee_fkey
        FOREIGN KEY (tenant_id, assignee_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE SET NULL (assignee_id);

CREATE INDEX idx_feedbacks_assignee_id ON feedbacks (tenant_id, assignee_id)
    WHERE assignee_id IS NOT NULL;
CREATE INDEX idx_issues_assignee_id ON issues (tenant_id, assignee_id)
    WHERE assignee_id IS NOT NULL;

CREATE TABLE tags (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    name citext NOT NULL,
    color text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tags_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT tags_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT tags_tenant_name_key UNIQUE (tenant_id, name),
    CONSTRAINT tags_name_valid CHECK (
        name::text = btrim(name::text) AND char_length(name::text) BETWEEN 1 AND 40
    ),
    CONSTRAINT tags_color_valid CHECK (color ~ '^#[0-9A-Fa-f]{6}$')
);

CREATE TRIGGER tags_set_updated_at
BEFORE UPDATE ON tags
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE feedback_tags (
    tenant_id uuid NOT NULL,
    feedback_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feedback_tags_pkey PRIMARY KEY (tenant_id, feedback_id, tag_id),
    CONSTRAINT feedback_tags_tenant_feedback_fkey
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT feedback_tags_tenant_tag_fkey
        FOREIGN KEY (tenant_id, tag_id)
        REFERENCES tags(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_feedback_tags_tag_id ON feedback_tags (tenant_id, tag_id);

CREATE TABLE issue_tags (
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_tags_pkey PRIMARY KEY (tenant_id, issue_id, tag_id),
    CONSTRAINT issue_tags_tenant_issue_fkey
        FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_tags_tenant_tag_fkey
        FOREIGN KEY (tenant_id, tag_id)
        REFERENCES tags(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_issue_tags_tag_id ON issue_tags (tenant_id, tag_id);

ALTER TABLE tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE tags FORCE ROW LEVEL SECURITY;
CREATE POLICY tags_tenant_isolation ON tags
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE feedback_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY feedback_tags_tenant_isolation ON feedback_tags
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE issue_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_tags_tenant_isolation ON issue_tags
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tags TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON feedback_tags TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON issue_tags TO fidibeki_app, fidibeki_admin;
