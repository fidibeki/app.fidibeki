DROP INDEX IF EXISTS idx_attachments_incomplete_upload_activity;

ALTER TABLE attachments
    DROP COLUMN upload_refreshed_at;
