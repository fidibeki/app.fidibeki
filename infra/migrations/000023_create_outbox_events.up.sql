CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    event event_type NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('feedback', 'issue')),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    dispatched_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    CONSTRAINT outbox_events_resource_key UNIQUE (event, resource_type, resource_id)
);

CREATE INDEX idx_outbox_events_tenant_id ON outbox_events (tenant_id);
CREATE INDEX idx_outbox_events_pending ON outbox_events (created_at, id) WHERE dispatched_at IS NULL;

ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;

CREATE POLICY outbox_events_tenant_isolation ON outbox_events
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON outbox_events TO fidibeki_app, fidibeki_admin;
