ALTER TABLE outbox_events
    ADD COLUMN event_key text NOT NULL DEFAULT '',
    ADD CONSTRAINT outbox_events_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_key;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_key
    UNIQUE (event, resource_type, resource_id, event_key);

CREATE TABLE issue_updates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    member_id uuid,
    old_status issue_status,
    new_status issue_status NOT NULL,
    comment text CHECK (comment IS NULL OR char_length(comment) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_updates_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT issue_updates_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_updates_tenant_member_fkey FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL,
    CONSTRAINT issue_updates_status_changed CHECK (old_status IS NULL OR old_status <> new_status)
);

CREATE INDEX idx_issue_updates_tenant_id ON issue_updates (tenant_id);
CREATE INDEX idx_issue_updates_issue_id ON issue_updates (issue_id);
CREATE INDEX idx_issue_updates_created_at ON issue_updates (tenant_id, created_at DESC);

ALTER TABLE issue_updates ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_updates FORCE ROW LEVEL SECURITY;

CREATE POLICY issue_updates_tenant_isolation ON issue_updates
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON issue_updates TO fidibeki_app, fidibeki_admin;
