DELETE FROM retention_maintenance_status
WHERE operation_key = 'email_feedback_invitations';

ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    DROP CONSTRAINT retention_maintenance_status_category_check,
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
            'identity_asset_deletions',
            'saml_auth_artifacts',
            'mobile_capture_sessions'
        )),
    ADD CONSTRAINT retention_maintenance_status_category_check
        CHECK (category IN (
            'auth_magic_links',
            'attachments',
            'export_archives',
            'privacy_rights',
            'session_replays',
            'widget_events',
            'widget_daily',
            'abuse_diagnostics',
            'ai_processing',
            'identity_assets',
            'mobile_capture_credentials'
        ));

DROP INDEX IF EXISTS idx_email_feedback_invitations_cleanup;

ALTER TABLE email_feedback_invitations
    DROP CONSTRAINT email_feedback_invitations_cleanup_bounded,
    ALTER COLUMN cleanup_after DROP NOT NULL;
