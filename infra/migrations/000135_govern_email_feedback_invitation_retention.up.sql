UPDATE email_feedback_invitations
SET cleanup_after = LEAST(
    COALESCE(cleanup_after, expires_at + interval '90 days'),
    expires_at + interval '90 days'
);

ALTER TABLE email_feedback_invitations
    ALTER COLUMN cleanup_after SET NOT NULL,
    ADD CONSTRAINT email_feedback_invitations_cleanup_bounded CHECK (
        cleanup_after >= expires_at
        AND cleanup_after <= expires_at + interval '90 days'
    );

CREATE INDEX idx_email_feedback_invitations_cleanup
    ON email_feedback_invitations (cleanup_after, id);

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
            'mobile_capture_sessions',
            'email_feedback_invitations'
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
            'mobile_capture_credentials',
            'email_feedback_delivery'
        ));

INSERT INTO retention_maintenance_status (
    operation_key, category, expected_interval_seconds, alert_grace_seconds
) VALUES ('email_feedback_invitations', 'email_feedback_delivery', 86400, 900);
