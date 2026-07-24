DELETE FROM privacy_jobs WHERE kind = 'member_export';

DROP INDEX IF EXISTS idx_privacy_jobs_case;

ALTER TABLE privacy_jobs
    DROP CONSTRAINT IF EXISTS privacy_jobs_kind_request_consistent,
    DROP CONSTRAINT IF EXISTS privacy_jobs_tenant_case_fkey,
    DROP COLUMN IF EXISTS privacy_case_id;

ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_kind_request_consistent CHECK (
    (kind = 'workspace_export' AND reporter_request_id IS NULL)
    OR (kind IN ('reporter_export', 'reporter_anonymize') AND reporter_request_id IS NOT NULL)
);
