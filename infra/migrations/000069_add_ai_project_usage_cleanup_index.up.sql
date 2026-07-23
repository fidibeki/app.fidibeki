CREATE INDEX idx_ai_project_daily_usage_cleanup
    ON ai_project_daily_usage (usage_date, project_id);
