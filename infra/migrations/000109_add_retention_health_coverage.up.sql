-- Extend the bounded retention health ledger from the two magic-link cleaners
-- to every enforced cleaner. Same contract as migration 105: one current-state
-- row per code-owned operation with counters, timestamps, and the generic
-- failure code only — never tenant, person, or credential data. Grants are
-- unchanged.

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
            'identity_asset_deletions'
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
            'identity_assets'
        ));

INSERT INTO retention_maintenance_status (
    operation_key,
    category,
    expected_interval_seconds,
    alert_grace_seconds
) VALUES
    ('attachments', 'attachments', 86400, 900),
    ('privacy_export_archives', 'export_archives', 86400, 900),
    ('reporter_privacy_requests', 'privacy_rights', 86400, 900),
    ('replay_sessions', 'session_replays', 86400, 900),
    ('widget_funnel_events', 'widget_events', 3600, 900),
    ('widget_funnel_daily', 'widget_daily', 3600, 900),
    ('abuse_diagnostics', 'abuse_diagnostics', 3600, 900),
    ('ai_budget_ledgers', 'ai_processing', 86400, 900),
    ('identity_asset_deletions', 'identity_assets', 60, 900);
