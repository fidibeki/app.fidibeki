CREATE TYPE abuse_diagnostic_category AS ENUM (
    'rate_limit',
    'spam',
    'plan_quota',
    'upload_safety',
    'ai_budget',
    'provider'
);

CREATE TABLE abuse_diagnostics_hourly (
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    bucket_start timestamptz NOT NULL,
    category abuse_diagnostic_category NOT NULL,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 64),
    event_count bigint NOT NULL DEFAULT 1 CHECK (event_count > 0),
    first_occurred_at timestamptz NOT NULL,
    last_occurred_at timestamptz NOT NULL,
    CONSTRAINT abuse_diagnostics_hourly_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT abuse_diagnostics_hourly_bucket_is_hour CHECK (
        bucket_start = date_trunc('hour', bucket_start AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
    ),
    CONSTRAINT abuse_diagnostics_hourly_occurrence_order CHECK (
        first_occurred_at <= last_occurred_at
    ),
    CONSTRAINT abuse_diagnostics_hourly_key
        PRIMARY KEY (project_id, bucket_start, category, reason)
);

CREATE INDEX idx_abuse_diagnostics_tenant_bucket
    ON abuse_diagnostics_hourly (tenant_id, bucket_start DESC);
CREATE INDEX idx_abuse_diagnostics_retention
    ON abuse_diagnostics_hourly (bucket_start, project_id, category, reason);

ALTER TABLE abuse_diagnostics_hourly ENABLE ROW LEVEL SECURITY;
ALTER TABLE abuse_diagnostics_hourly FORCE ROW LEVEL SECURITY;

CREATE POLICY abuse_diagnostics_hourly_tenant_isolation ON abuse_diagnostics_hourly
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE abuse_diagnostic_category TO fidibeki_app, fidibeki_admin;
GRANT SELECT ON abuse_diagnostics_hourly TO fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON abuse_diagnostics_hourly TO fidibeki_admin;
