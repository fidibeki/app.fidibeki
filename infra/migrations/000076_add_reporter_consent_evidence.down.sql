DROP INDEX IF EXISTS idx_issues_reporter_consent_active;
DROP INDEX IF EXISTS idx_feedbacks_reporter_consent_active;

ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_reporter_consent_evidence_consistent,
    DROP COLUMN IF EXISTS reporter_consent_purpose,
    DROP COLUMN IF EXISTS reporter_consent_notice_version,
    DROP COLUMN IF EXISTS reporter_consent_locale,
    DROP COLUMN IF EXISTS reporter_consent_source;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_reporter_consent_evidence_consistent,
    DROP COLUMN IF EXISTS reporter_consent_purpose,
    DROP COLUMN IF EXISTS reporter_consent_notice_version,
    DROP COLUMN IF EXISTS reporter_consent_locale,
    DROP COLUMN IF EXISTS reporter_consent_source;
