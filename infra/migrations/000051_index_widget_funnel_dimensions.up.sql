CREATE INDEX idx_widget_funnel_daily_tenant_rule_date
    ON widget_funnel_daily (tenant_id, rule_id, event_date DESC);

CREATE INDEX idx_widget_funnel_daily_tenant_page_date
    ON widget_funnel_daily (tenant_id, page_key, event_date DESC);
