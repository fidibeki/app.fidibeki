ALTER TABLE widget_rules
    DROP CONSTRAINT widget_rules_project_definition_key_key,
    DROP CONSTRAINT widget_rules_localizations_object,
    DROP CONSTRAINT widget_rules_response_blocks_array,
    DROP CONSTRAINT widget_rules_definition_version_valid,
    DROP CONSTRAINT widget_rules_definition_key_valid,
    DROP COLUMN collect_contact,
    DROP COLUMN allow_comment,
    DROP COLUMN localizations,
    DROP COLUMN response_blocks,
    DROP COLUMN objective,
    DROP COLUMN presentation,
    DROP COLUMN definition_status,
    DROP COLUMN definition_version,
    DROP COLUMN definition_key;

ALTER TABLE projects
    DROP CONSTRAINT projects_default_locale_valid,
    DROP COLUMN default_locale;

DROP TYPE widget_objective;
DROP TYPE widget_presentation;
DROP TYPE widget_definition_status;

