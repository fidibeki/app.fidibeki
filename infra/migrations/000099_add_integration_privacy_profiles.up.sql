ALTER TABLE integrations
    ADD COLUMN privacy_fields text[] NOT NULL DEFAULT '{}',
    ADD COLUMN privacy_profile_version smallint NOT NULL DEFAULT 1
        CHECK (privacy_profile_version = 1),
    ADD CONSTRAINT integrations_privacy_fields_allowed CHECK (
        privacy_fields <@ ARRAY[
            'description',
            'page',
            'rating',
            'classification',
            'workflow',
            'attachments'
        ]::text[]
    );

UPDATE integrations
SET privacy_fields = ARRAY[
    'description',
    'page',
    'rating',
    'classification',
    'workflow',
    'attachments'
]::text[];

ALTER TABLE delivery_logs
    ADD COLUMN destination_kind integration_kind,
    ADD COLUMN privacy_profile_version smallint NOT NULL DEFAULT 1
        CHECK (privacy_profile_version = 1),
    ADD COLUMN data_categories text[] NOT NULL DEFAULT '{}',
    ADD COLUMN destination_capabilities jsonb NOT NULL DEFAULT '{
        "message_update": false,
        "message_delete": false,
        "message_recall": false,
        "fidibeki_links_require_authentication": true,
        "fidibeki_links_access_controlled": true
    }'::jsonb,
    ADD CONSTRAINT delivery_logs_data_categories_allowed CHECK (
        data_categories <@ ARRAY[
            'event_metadata',
            'resource_identity',
            'sanitized_title',
            'project',
            'workflow_status',
            'priority',
            'fidibeki_link',
            'description',
            'source_page',
            'rating',
            'classification',
            'workflow_context',
            'attachment_metadata'
        ]::text[]
    ),
    ADD CONSTRAINT delivery_logs_destination_capabilities_object CHECK (
        jsonb_typeof(destination_capabilities) = 'object'
    );

UPDATE delivery_logs AS delivery
SET destination_kind = integration.kind
FROM integrations AS integration
WHERE integration.id = delivery.integration_id
  AND integration.tenant_id = delivery.tenant_id;
