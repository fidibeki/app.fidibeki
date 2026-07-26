DROP INDEX IF EXISTS idx_integrations_slack_install;

ALTER TABLE integrations
    DROP CONSTRAINT IF EXISTS integrations_manual_has_no_oauth_fields,
    DROP CONSTRAINT IF EXISTS integrations_oauth_complete,
    DROP CONSTRAINT IF EXISTS integrations_oauth_is_slack,
    DROP COLUMN IF EXISTS slack_access_token_ciphertext,
    DROP COLUMN IF EXISTS slack_channel_id,
    DROP COLUMN IF EXISTS slack_team_name,
    DROP COLUMN IF EXISTS slack_team_id,
    DROP COLUMN IF EXISTS install_source;

DROP TYPE IF EXISTS integration_install_source;
