CREATE TYPE widget_funnel_event_type AS ENUM (
    'matched', 'impression', 'interaction', 'submitted', 'dismissed'
);

CREATE TABLE widget_funnel_events (
    event_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    rule_name text NOT NULL CHECK (char_length(rule_name) BETWEEN 1 AND 120),
    event_type widget_funnel_event_type NOT NULL,
    visitor_id uuid NOT NULL,
    page_view_id uuid NOT NULL,
    page_url text NOT NULL CHECK (char_length(page_url) BETWEEN 1 AND 4096),
    page_key bytea NOT NULL CHECK (octet_length(page_key) = 32),
    feedback_id uuid,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT widget_funnel_events_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT widget_funnel_events_tenant_feedback_fkey
        FOREIGN KEY (tenant_id, feedback_id) REFERENCES feedbacks(tenant_id, id)
        ON DELETE SET NULL (feedback_id),
    CONSTRAINT widget_funnel_events_feedback_consistent CHECK (
        event_type = 'submitted' OR feedback_id IS NULL
    ),
    CONSTRAINT widget_funnel_events_lifecycle_key
        UNIQUE (project_id, rule_id, page_view_id, event_type)
);

CREATE INDEX idx_widget_funnel_events_tenant_occurred
    ON widget_funnel_events (tenant_id, occurred_at DESC);
CREATE INDEX idx_widget_funnel_events_project_occurred
    ON widget_funnel_events (project_id, occurred_at DESC);
CREATE INDEX idx_widget_funnel_events_retention
    ON widget_funnel_events (received_at, event_id);

CREATE TABLE widget_funnel_daily (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    rule_name text NOT NULL CHECK (char_length(rule_name) BETWEEN 1 AND 120),
    page_url text NOT NULL CHECK (char_length(page_url) BETWEEN 1 AND 4096),
    page_key bytea NOT NULL CHECK (octet_length(page_key) = 32),
    event_date date NOT NULL,
    matched_count bigint NOT NULL DEFAULT 0 CHECK (matched_count >= 0),
    impression_count bigint NOT NULL DEFAULT 0 CHECK (impression_count >= 0),
    interaction_count bigint NOT NULL DEFAULT 0 CHECK (interaction_count >= 0),
    submitted_count bigint NOT NULL DEFAULT 0 CHECK (submitted_count >= 0),
    dismissed_count bigint NOT NULL DEFAULT 0 CHECK (dismissed_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT widget_funnel_daily_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT widget_funnel_daily_key PRIMARY KEY (project_id, rule_id, event_date, page_key)
);

CREATE INDEX idx_widget_funnel_daily_tenant_date
    ON widget_funnel_daily (tenant_id, event_date DESC);
CREATE INDEX idx_widget_funnel_daily_project_date
    ON widget_funnel_daily (project_id, event_date DESC);

ALTER TABLE widget_funnel_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE widget_funnel_events FORCE ROW LEVEL SECURITY;
ALTER TABLE widget_funnel_daily ENABLE ROW LEVEL SECURITY;
ALTER TABLE widget_funnel_daily FORCE ROW LEVEL SECURITY;

CREATE POLICY widget_funnel_events_tenant_isolation ON widget_funnel_events
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY widget_funnel_daily_tenant_isolation ON widget_funnel_daily
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE widget_funnel_event_type TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON widget_funnel_events TO fidibeki_app;
GRANT SELECT, INSERT, UPDATE ON widget_funnel_daily TO fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON widget_funnel_events TO fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON widget_funnel_daily TO fidibeki_admin;
