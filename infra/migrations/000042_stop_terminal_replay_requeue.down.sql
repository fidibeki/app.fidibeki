DROP INDEX IF EXISTS idx_sessions_merge_pending;

CREATE INDEX idx_sessions_merge_pending
    ON sessions (merge_requested_at, id)
    WHERE merge_requested_at IS NOT NULL AND merged_at IS NULL;

ALTER TABLE session_batches DROP COLUMN upload_refreshed_at;
