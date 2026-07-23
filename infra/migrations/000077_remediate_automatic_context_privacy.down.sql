-- The data minimisation in the up migration is intentionally irreversible: restoring URLs,
-- referrers, raw user agents, DOM captures, or invalidated export contents would reintroduce
-- data that the privacy contract requires Fidibeki not to retain.
ALTER TABLE sessions
    ALTER COLUMN replay_privacy_version SET DEFAULT 1;

UPDATE sessions
SET replay_privacy_version = 0
WHERE replay_privacy_version > 1;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_replay_privacy_version_check,
    ADD CONSTRAINT sessions_replay_privacy_version_check
        CHECK (replay_privacy_version BETWEEN 0 AND 1);

DROP INDEX IF EXISTS idx_sessions_replay_privacy_cleanup;

CREATE INDEX idx_sessions_replay_privacy_cleanup
    ON sessions (id)
    WHERE retention_deleted_at IS NULL AND replay_privacy_version < 1;

DROP INDEX IF EXISTS idx_privacy_jobs_context_privacy_cleanup;

ALTER TABLE privacy_jobs
    DROP CONSTRAINT IF EXISTS privacy_jobs_context_privacy_version_valid,
    DROP COLUMN IF EXISTS context_privacy_version;
