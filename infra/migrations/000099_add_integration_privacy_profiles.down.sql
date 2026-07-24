ALTER TABLE delivery_logs
    DROP CONSTRAINT delivery_logs_destination_capabilities_object,
    DROP CONSTRAINT delivery_logs_data_categories_allowed,
    DROP COLUMN destination_capabilities,
    DROP COLUMN data_categories,
    DROP COLUMN privacy_profile_version,
    DROP COLUMN destination_kind;

ALTER TABLE integrations
    DROP CONSTRAINT integrations_privacy_fields_allowed,
    DROP COLUMN privacy_profile_version,
    DROP COLUMN privacy_fields;
