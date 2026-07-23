DROP INDEX IF EXISTS idx_sessions_replay_consent;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_tenant_replay_consent_fkey,
    DROP COLUMN IF EXISTS replay_consent_id;

DROP TABLE IF EXISTS replay_consents;
DROP TYPE IF EXISTS replay_consent_state;

ALTER TABLE widget_rules
    DROP CONSTRAINT IF EXISTS widget_rules_replay_consent_object,
    DROP COLUMN IF EXISTS replay_consent;
