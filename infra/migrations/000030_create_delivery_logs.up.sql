CREATE TABLE delivery_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    outbox_event_id uuid NOT NULL,
    event event_type NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('feedback', 'issue')),
    resource_id uuid NOT NULL,
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    status delivery_status NOT NULL DEFAULT 'pending',
    provider_message_id text,
    error_message text CHECK (error_message IS NULL OR char_length(error_message) <= 2000),
    sent_at timestamptz,
    webhook_event_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT delivery_logs_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT delivery_logs_tenant_outbox_fkey FOREIGN KEY (tenant_id, outbox_event_id)
        REFERENCES outbox_events(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT delivery_logs_outbox_attempt_key UNIQUE (outbox_event_id, attempt_number),
    CONSTRAINT delivery_logs_sent_consistent CHECK (
        (status IN ('sent', 'bounced') AND provider_message_id IS NOT NULL AND sent_at IS NOT NULL) OR
        (status NOT IN ('sent', 'bounced'))
    )
);

CREATE INDEX idx_delivery_logs_tenant_id ON delivery_logs (tenant_id);
CREATE INDEX idx_delivery_logs_resource ON delivery_logs (resource_type, resource_id);
CREATE INDEX idx_delivery_logs_status ON delivery_logs (tenant_id, status);
CREATE INDEX idx_delivery_logs_created_at ON delivery_logs (tenant_id, created_at DESC);
CREATE INDEX idx_delivery_logs_provider_message_id ON delivery_logs (provider_message_id)
    WHERE provider_message_id IS NOT NULL;

ALTER TABLE delivery_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE delivery_logs FORCE ROW LEVEL SECURITY;

CREATE POLICY delivery_logs_tenant_isolation ON delivery_logs
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON delivery_logs TO fidibeki_app, fidibeki_admin;
