CREATE INDEX idx_widget_funnel_events_project_page_occurred
    ON widget_funnel_events (project_id, page_key, occurred_at, event_id);

CREATE INDEX idx_widget_funnel_daily_retention
    ON widget_funnel_daily (event_date, project_id, rule_id, page_key);

CREATE TABLE widget_funnel_maintenance (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_reconciled_at timestamptz
);

INSERT INTO widget_funnel_maintenance (singleton) VALUES (true);

GRANT SELECT, UPDATE ON widget_funnel_maintenance TO fidibeki_admin;
