-- Stripped credentials/query/fragment cannot be recovered; this backfill is intentionally
-- one-way (issue #53).
DROP INDEX IF EXISTS idx_sessions_replay_privacy_cleanup;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_replay_privacy_version_check,
    DROP COLUMN IF EXISTS replay_privacy_version;
