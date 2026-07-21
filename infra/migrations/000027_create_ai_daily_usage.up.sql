CREATE TABLE ai_daily_usage (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    usage_date date NOT NULL,
    requests integer NOT NULL DEFAULT 0 CHECK (requests >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, usage_date)
);

CREATE INDEX idx_ai_daily_usage_date ON ai_daily_usage (usage_date);

CREATE TRIGGER ai_daily_usage_set_updated_at
BEFORE UPDATE ON ai_daily_usage
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE ai_daily_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_daily_usage FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_daily_usage_tenant_isolation ON ai_daily_usage
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_daily_usage TO fidibeki_app, fidibeki_admin;
