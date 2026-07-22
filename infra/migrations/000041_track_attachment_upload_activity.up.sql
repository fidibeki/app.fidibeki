ALTER TABLE attachments
    ADD COLUMN upload_refreshed_at timestamptz;

UPDATE attachments
SET upload_refreshed_at = created_at;

ALTER TABLE attachments
    ALTER COLUMN upload_refreshed_at SET DEFAULT now(),
    ALTER COLUMN upload_refreshed_at SET NOT NULL;

CREATE INDEX idx_attachments_incomplete_upload_activity
    ON attachments (upload_refreshed_at, id)
    WHERE completed_at IS NULL
      AND processing_status IN ('pending', 'processing');
