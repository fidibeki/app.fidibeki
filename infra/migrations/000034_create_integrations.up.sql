CREATE TABLE integrations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    kind integration_kind NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    webhook_url text NOT NULL CHECK (char_length(webhook_url) BETWEEN 1 AND 2048),
    channel text CHECK (channel IS NULL OR char_length(channel) BETWEEN 1 AND 100),
    custom_headers_ciphertext bytea,
    webhook_secret_ciphertext bytea,
    webhook_secret_fingerprint text CHECK (
        webhook_secret_fingerprint IS NULL OR char_length(webhook_secret_fingerprint) BETWEEN 8 AND 80
    ),
    events event_type[] NOT NULL DEFAULT '{}',
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT integrations_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT integrations_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT integrations_webhook_secret_consistent CHECK (
        (kind = 'webhook' AND webhook_secret_ciphertext IS NOT NULL AND webhook_secret_fingerprint IS NOT NULL) OR
        (kind <> 'webhook' AND webhook_secret_ciphertext IS NULL AND webhook_secret_fingerprint IS NULL)
    ),
    CONSTRAINT integrations_custom_headers_kind CHECK (
        custom_headers_ciphertext IS NULL OR kind = 'webhook'
    )
);

CREATE INDEX idx_integrations_tenant_id ON integrations (tenant_id);
CREATE INDEX idx_integrations_kind ON integrations (tenant_id, kind);
CREATE INDEX idx_integrations_active ON integrations (tenant_id, is_active) WHERE is_active = true;
CREATE INDEX idx_integrations_events ON integrations USING gin (events) WHERE is_active = true;

ALTER TABLE integrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integrations FORCE ROW LEVEL SECURITY;

CREATE POLICY integrations_tenant_isolation ON integrations
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON integrations TO fidibeki_app, fidibeki_admin;
