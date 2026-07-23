ALTER TABLE widget_rules
    DROP CONSTRAINT widget_rules_widget_appearance_overrides_object,
    DROP COLUMN widget_appearance_overrides;

ALTER TABLE projects
    DROP CONSTRAINT projects_widget_appearance_overrides_object,
    DROP COLUMN widget_appearance_overrides;

ALTER TABLE tenants
    DROP CONSTRAINT tenants_widget_appearance_defaults_object,
    DROP COLUMN widget_appearance_defaults;

REVOKE UPDATE (reporter_access_enabled) ON tenants FROM fidibeki_app;
