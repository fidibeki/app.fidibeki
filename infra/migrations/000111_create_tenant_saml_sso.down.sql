DELETE FROM retention_maintenance_status WHERE operation_key = 'saml_auth_artifacts';

ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    ADD CONSTRAINT retention_maintenance_status_operation_key_check
        CHECK (operation_key IN (
            'tenant_magic_links',
            'platform_admin_magic_links',
            'attachments',
            'privacy_export_archives',
            'reporter_privacy_requests',
            'replay_sessions',
            'widget_funnel_events',
            'widget_funnel_daily',
            'abuse_diagnostics',
            'ai_budget_ledgers',
            'identity_asset_deletions'
        ));

ALTER TABLE tenant_magic_links
    DROP CONSTRAINT tenant_magic_links_saml_identity_consistency,
    DROP COLUMN saml_subject_id,
    DROP COLUMN saml_idp_entity_id,
    DROP COLUMN authentication_method;

ALTER TABLE tenant_member_sessions
    DROP CONSTRAINT tenant_member_sessions_saml_identity_consistency,
    DROP COLUMN saml_subject_id,
    DROP COLUMN saml_idp_entity_id,
    DROP COLUMN authentication_method;

DROP TABLE tenant_saml_assertions;
DROP TABLE tenant_saml_auth_requests;
DROP TABLE tenant_saml_certificates;
DROP TABLE tenant_saml_configurations;

DROP TYPE tenant_sso_mode;
