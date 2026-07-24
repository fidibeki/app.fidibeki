DROP TABLE IF EXISTS widget_analytics_consents;
DROP TYPE IF EXISTS widget_analytics_consent_state;

ALTER TABLE widget_rules
    DROP CONSTRAINT IF EXISTS widget_rules_analytics_consent_object,
    DROP COLUMN IF EXISTS analytics_consent;
