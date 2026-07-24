DROP INDEX IF EXISTS idx_privacy_recipient_actions_integration_action;

ALTER TABLE privacy_recipient_actions
    DROP CONSTRAINT privacy_recipient_actions_tenant_integration_action_fkey,
    DROP COLUMN privacy_integration_action_id;

DROP TABLE IF EXISTS privacy_integration_actions;
