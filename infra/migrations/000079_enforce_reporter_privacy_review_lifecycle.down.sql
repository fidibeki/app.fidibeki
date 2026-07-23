-- Move any in-flight manual-review rows to the pre-existing 'failed' state (documented via
-- error_message) so the older, stricter state_consistent constraints below can be restored
-- without violating existing rows. Administrators retry failed reporter requests through the
-- existing retry path after a rollback.
UPDATE privacy_jobs
SET status = 'failed', error_message = COALESCE(error_message, 'reporter privacy review lifecycle rolled back')
WHERE status = 'awaiting_manual_review';

UPDATE reporter_privacy_requests
SET status = 'failed', error_message = COALESCE(error_message, 'reporter privacy review lifecycle rolled back')
WHERE status = 'awaiting_manual_review';

DROP INDEX IF EXISTS idx_reporter_privacy_requests_awaiting_review;

ALTER TABLE reporter_privacy_requests DROP CONSTRAINT reporter_privacy_requests_state_consistent;
ALTER TABLE reporter_privacy_requests ADD CONSTRAINT reporter_privacy_requests_state_consistent CHECK (
    (status = 'pending_verification' AND reporter_email IS NOT NULL
        AND verified_at IS NULL AND access_expires_at IS NULL AND completed_at IS NULL)
    OR (status = 'processing' AND reporter_email IS NOT NULL
        AND verified_at IS NOT NULL AND access_expires_at IS NOT NULL AND completed_at IS NULL)
    OR (status = 'completed' AND verified_at IS NOT NULL
        AND access_expires_at IS NOT NULL AND completed_at IS NOT NULL
        AND (action = 'export' OR reporter_email IS NULL))
    OR (status = 'failed' AND verified_at IS NOT NULL
        AND access_expires_at IS NOT NULL AND completed_at IS NULL
        AND error_message IS NOT NULL)
    OR (status = 'expired' AND reporter_email IS NULL AND (
        (verified_at IS NULL AND access_expires_at IS NULL)
        OR (verified_at IS NOT NULL AND access_expires_at IS NOT NULL)
    ))
);

ALTER TABLE privacy_jobs DROP CONSTRAINT privacy_jobs_state_consistent;
ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_state_consistent CHECK (
    (status = 'pending' AND started_at IS NULL AND completed_at IS NULL
        AND expires_at IS NULL AND object_key IS NULL AND object_size_bytes IS NULL)
    OR (status = 'processing' AND started_at IS NOT NULL AND heartbeat_at IS NOT NULL
        AND completed_at IS NULL AND expires_at IS NULL AND object_key IS NULL
        AND object_size_bytes IS NULL)
    OR (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
        AND expires_at IS NOT NULL AND (
            (kind = 'reporter_anonymize' AND object_key IS NULL AND object_size_bytes = 0)
            OR (kind <> 'reporter_anonymize' AND object_key IS NOT NULL
                AND object_size_bytes IS NOT NULL)
        ))
    OR (status = 'failed' AND completed_at IS NULL AND expires_at IS NULL
        AND object_key IS NULL AND object_size_bytes IS NULL AND error_message IS NOT NULL)
    OR (status = 'expired' AND completed_at IS NOT NULL AND expires_at IS NOT NULL
        AND object_key IS NULL AND object_size_bytes IS NOT NULL)
);

-- PostgreSQL enum labels cannot be removed in place; migration 000078 adds them idempotently,
-- so reapplying this pair of migrations after a rollback is safe.
