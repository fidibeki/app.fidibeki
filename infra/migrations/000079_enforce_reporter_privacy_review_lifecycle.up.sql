-- Automated anonymization succeeding is no longer sufficient for completion when free-form
-- content still needs a documented human decision. awaiting_manual_review is a non-terminal
-- state: no completed_at, no expiry, no archive, so nothing communicates false completion.
ALTER TABLE privacy_jobs DROP CONSTRAINT privacy_jobs_state_consistent;
ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_state_consistent CHECK (
    (status = 'pending' AND started_at IS NULL AND completed_at IS NULL
        AND expires_at IS NULL AND object_key IS NULL AND object_size_bytes IS NULL)
    OR (status = 'processing' AND started_at IS NOT NULL AND heartbeat_at IS NOT NULL
        AND completed_at IS NULL AND expires_at IS NULL AND object_key IS NULL
        AND object_size_bytes IS NULL)
    OR (status = 'awaiting_manual_review' AND kind = 'reporter_anonymize'
        AND started_at IS NOT NULL AND completed_at IS NULL AND expires_at IS NULL
        AND object_key IS NULL AND object_size_bytes IS NULL)
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

ALTER TABLE reporter_privacy_requests DROP CONSTRAINT reporter_privacy_requests_state_consistent;
ALTER TABLE reporter_privacy_requests ADD CONSTRAINT reporter_privacy_requests_state_consistent CHECK (
    (status = 'pending_verification' AND reporter_email IS NOT NULL
        AND verified_at IS NULL AND access_expires_at IS NULL AND completed_at IS NULL)
    OR (status = 'processing' AND reporter_email IS NOT NULL
        AND verified_at IS NOT NULL AND access_expires_at IS NOT NULL AND completed_at IS NULL)
    OR (status = 'awaiting_manual_review' AND action = 'anonymize' AND reporter_email IS NULL
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

CREATE INDEX idx_reporter_privacy_requests_awaiting_review
    ON reporter_privacy_requests (tenant_id, id)
    WHERE status = 'awaiting_manual_review';

-- Reconciliation: a request previously marked completed while manual_review_count was positive
-- claimed a finality it never earned. Reopen it as awaiting_manual_review and reopen its
-- terminal job alongside it. The original scan already discarded the per-record linkage (the
-- anonymize job nulls reporter_email unconditionally), so the exact flagged resources can no
-- longer be identified without re-associating them to the reporter's raw email, which this
-- migration must not do. A single legacy_unlinked review item documents the outstanding
-- obligation so a human can close it with a retained-exception rationale or other lawful
-- documented decision instead of silently keeping a false "completed" status.
WITH legacy AS (
    UPDATE reporter_privacy_requests
    SET status = 'awaiting_manual_review'
    WHERE action = 'anonymize' AND status = 'completed' AND manual_review_count > 0
    RETURNING id, tenant_id
), reopened_jobs AS (
    UPDATE privacy_jobs AS job
    SET status = 'awaiting_manual_review', completed_at = NULL, expires_at = NULL,
        object_key = NULL, object_size_bytes = NULL
    FROM legacy
    WHERE job.tenant_id = legacy.tenant_id AND job.reporter_request_id = legacy.id
      AND job.kind = 'reporter_anonymize' AND job.status = 'completed'
    RETURNING job.id AS job_id, job.tenant_id, legacy.id AS reporter_request_id
), inserted_items AS (
    INSERT INTO reporter_privacy_review_items (
        tenant_id, reporter_request_id, privacy_job_id, resource_type, resource_id, fields, status
    )
    SELECT legacy.tenant_id, legacy.id, reopened_jobs.job_id,
           'legacy_unlinked', legacy.id, ARRAY['manual_review_count']::text[], 'pending'
    FROM legacy
    LEFT JOIN reopened_jobs ON reopened_jobs.reporter_request_id = legacy.id
    ON CONFLICT (tenant_id, reporter_request_id, resource_type, resource_id) DO NOTHING
    RETURNING tenant_id, reporter_request_id
)
INSERT INTO tenant_access_events (tenant_id, kind, actor_member_id, subject_member_id, data)
SELECT legacy.tenant_id, 'privacy.reporter_anonymization_reopened', NULL, NULL,
       jsonb_build_object('reporter_request_id', legacy.id::text, 'reason', 'migration_reconciliation')
FROM legacy;
