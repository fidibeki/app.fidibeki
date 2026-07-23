-- The data minimisation in the up migration is intentionally irreversible: restoring URLs,
-- referrers, raw user agents, DOM captures, or invalidated export contents would reintroduce
-- data that the privacy contract requires Fidibeki not to retain.
DROP INDEX IF EXISTS idx_privacy_jobs_context_privacy_cleanup;

ALTER TABLE privacy_jobs
    DROP CONSTRAINT IF EXISTS privacy_jobs_context_privacy_version_valid,
    DROP COLUMN IF EXISTS context_privacy_version;
