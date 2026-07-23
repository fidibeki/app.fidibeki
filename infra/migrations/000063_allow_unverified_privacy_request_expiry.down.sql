DELETE FROM reporter_privacy_requests
WHERE status = 'expired' AND verified_at IS NULL;

ALTER TABLE reporter_privacy_requests
    DROP CONSTRAINT reporter_privacy_requests_state_consistent;

ALTER TABLE reporter_privacy_requests
    ADD CONSTRAINT reporter_privacy_requests_state_consistent CHECK (
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
        OR (status = 'expired' AND reporter_email IS NULL AND access_expires_at IS NOT NULL)
    );
