ALTER TABLE session_batches
    ADD COLUMN upload_refreshed_at timestamptz NOT NULL DEFAULT now();

UPDATE session_batches SET upload_refreshed_at = created_at;

DROP INDEX IF EXISTS idx_sessions_merge_pending;

CREATE INDEX idx_sessions_merge_pending
    ON sessions (merge_requested_at, id)
    WHERE merge_requested_at IS NOT NULL
      AND merged_at IS NULL
      AND merge_attempts < 12;
