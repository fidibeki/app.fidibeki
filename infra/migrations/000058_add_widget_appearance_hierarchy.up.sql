ALTER TABLE tenants
    ADD COLUMN widget_appearance_defaults jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT tenants_widget_appearance_defaults_object CHECK (
        jsonb_typeof(widget_appearance_defaults) = 'object'
    );

ALTER TABLE projects
    ADD COLUMN widget_appearance_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT projects_widget_appearance_overrides_object CHECK (
        jsonb_typeof(widget_appearance_overrides) = 'object'
    );

ALTER TABLE widget_rules
    ADD COLUMN widget_appearance_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT widget_rules_widget_appearance_overrides_object CHECK (
        jsonb_typeof(widget_appearance_overrides) = 'object'
    );

UPDATE widget_rules
SET widget_appearance_overrides = jsonb_strip_nulls(jsonb_build_object(
    'position', position,
    'theme', theme,
    'accent_color', accent_color
));

GRANT UPDATE (reporter_access_enabled, widget_appearance_defaults) ON tenants TO fidibeki_app;
