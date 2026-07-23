DROP INDEX IF EXISTS idx_sessions_retention_pending;
DROP INDEX IF EXISTS idx_attachments_retention_pending;

DELETE FROM session_batches WHERE retention_deleted_at IS NOT NULL;
DELETE FROM sessions WHERE retention_deleted_at IS NOT NULL;
DELETE FROM attachments WHERE retention_deleted_at IS NOT NULL;

ALTER TABLE session_batches
    DROP CONSTRAINT IF EXISTS session_batches_retention_consistent,
    ALTER COLUMN storage_key SET NOT NULL,
    DROP COLUMN IF EXISTS retention_deleted_at;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_merge_consistent,
    ADD CONSTRAINT sessions_merge_consistent CHECK (
        (merged_at IS NULL AND storage_key IS NULL)
        OR (merged_at IS NOT NULL AND storage_key IS NOT NULL AND merge_requested_at IS NOT NULL)
    ),
    DROP COLUMN IF EXISTS retention_deleted_at;

ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_retention_consistent,
    ALTER COLUMN storage_key SET NOT NULL,
    DROP COLUMN IF EXISTS retention_deleted_at;

DROP INDEX IF EXISTS idx_privacy_jobs_active_reporter_request;
ALTER TABLE privacy_jobs
    DROP CONSTRAINT IF EXISTS privacy_jobs_state_consistent,
    DROP CONSTRAINT IF EXISTS privacy_jobs_kind_request_consistent,
    DROP CONSTRAINT IF EXISTS privacy_jobs_tenant_reporter_request_fkey,
    DROP COLUMN IF EXISTS reporter_request_id;

ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_state_consistent CHECK (
    (status = 'pending' AND started_at IS NULL AND completed_at IS NULL
        AND expires_at IS NULL AND object_key IS NULL AND object_size_bytes IS NULL)
    OR (status = 'processing' AND started_at IS NOT NULL AND heartbeat_at IS NOT NULL
        AND completed_at IS NULL AND expires_at IS NULL AND object_key IS NULL
        AND object_size_bytes IS NULL)
    OR (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
        AND expires_at IS NOT NULL AND object_key IS NOT NULL AND object_size_bytes IS NOT NULL)
    OR (status = 'failed' AND completed_at IS NULL AND expires_at IS NULL
        AND object_key IS NULL AND object_size_bytes IS NULL AND error_message IS NOT NULL)
    OR (status = 'expired' AND completed_at IS NOT NULL AND expires_at IS NOT NULL
        AND object_key IS NULL AND object_size_bytes IS NOT NULL)
);

DROP TABLE IF EXISTS reporter_privacy_requests;
DROP TYPE IF EXISTS reporter_privacy_status;
DROP TYPE IF EXISTS reporter_privacy_action;

DROP TRIGGER IF EXISTS issues_track_reporter_consent ON issues;
ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_reporter_consent_consistent,
    DROP COLUMN IF EXISTS reporter_consent_granted_at,
    DROP COLUMN IF EXISTS reporter_consent_withdrawn_at;

DROP TRIGGER IF EXISTS feedbacks_track_reporter_consent ON feedbacks;
ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_reporter_consent_consistent,
    DROP COLUMN IF EXISTS reporter_consent_granted_at,
    DROP COLUMN IF EXISTS reporter_consent_withdrawn_at;

DROP FUNCTION IF EXISTS track_reporter_consent();
