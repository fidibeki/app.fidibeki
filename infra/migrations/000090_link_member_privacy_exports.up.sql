ALTER TABLE privacy_jobs
    ADD COLUMN privacy_case_id uuid,
    ADD CONSTRAINT privacy_jobs_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE privacy_jobs DROP CONSTRAINT privacy_jobs_kind_request_consistent;
ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_kind_request_consistent CHECK (
    (
        kind = 'workspace_export'
        AND reporter_request_id IS NULL
        AND privacy_case_id IS NULL
    )
    OR (
        kind IN ('reporter_export', 'reporter_anonymize')
        AND reporter_request_id IS NOT NULL
        AND privacy_case_id IS NULL
    )
    OR (
        kind = 'member_export'
        AND reporter_request_id IS NULL
        AND privacy_case_id IS NOT NULL
    )
);

CREATE UNIQUE INDEX idx_privacy_jobs_case
    ON privacy_jobs (tenant_id, privacy_case_id)
    WHERE privacy_case_id IS NOT NULL;
