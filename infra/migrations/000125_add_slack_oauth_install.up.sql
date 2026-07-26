-- Slack one-click install. An OAuth-installed integration still delivers through
-- integrations.webhook_url exactly like a pasted one; these columns only record where
-- that URL came from and carry the bot token needed to revoke it on uninstall.
CREATE TYPE integration_install_source AS ENUM ('manual', 'oauth');

ALTER TABLE integrations
    ADD COLUMN install_source integration_install_source NOT NULL DEFAULT 'manual',
    ADD COLUMN slack_team_id text CHECK (
        slack_team_id IS NULL OR char_length(slack_team_id) BETWEEN 1 AND 64
    ),
    ADD COLUMN slack_team_name text CHECK (
        slack_team_name IS NULL OR char_length(slack_team_name) BETWEEN 1 AND 200
    ),
    ADD COLUMN slack_channel_id text CHECK (
        slack_channel_id IS NULL OR char_length(slack_channel_id) BETWEEN 1 AND 64
    ),
    ADD COLUMN slack_access_token_ciphertext bytea,
    -- Only Slack can be installed through OAuth today.
    ADD CONSTRAINT integrations_oauth_is_slack CHECK (
        install_source = 'manual' OR kind = 'slack'
    ),
    -- An OAuth row without a token could never be revoked, so refuse to store one.
    ADD CONSTRAINT integrations_oauth_complete CHECK (
        install_source <> 'oauth' OR (
            slack_team_id IS NOT NULL
            AND slack_channel_id IS NOT NULL
            AND slack_access_token_ciphertext IS NOT NULL
        )
    ),
    -- A manually pasted row must not carry OAuth provenance it never had.
    ADD CONSTRAINT integrations_manual_has_no_oauth_fields CHECK (
        install_source = 'oauth' OR (
            slack_team_id IS NULL
            AND slack_team_name IS NULL
            AND slack_channel_id IS NULL
            AND slack_access_token_ciphertext IS NULL
        )
    );

-- Re-installing the same workspace channel updates the existing row instead of
-- creating a second integration that would double-post every notification.
CREATE UNIQUE INDEX idx_integrations_slack_install
    ON integrations (tenant_id, slack_team_id, slack_channel_id)
    WHERE install_source = 'oauth';
