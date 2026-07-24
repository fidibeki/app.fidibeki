DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM feedbacks
        WHERE reporter_consent_notice_version = '2026-07-controller-v1'
        UNION ALL
        SELECT 1 FROM issues
        WHERE reporter_consent_notice_version = '2026-07-controller-v1'
    ) THEN
        RAISE EXCEPTION
            'cannot roll back contextual controller notices while v1 evidence exists';
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_issues_reporter_consent_active;
DROP INDEX IF EXISTS idx_feedbacks_reporter_consent_active;

ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_reporter_consent_evidence_consistent,
    DROP CONSTRAINT IF EXISTS issues_reporter_consent_notice_snapshot_valid,
    DROP COLUMN IF EXISTS reporter_consent_notice_snapshot;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_reporter_consent_evidence_consistent,
    DROP CONSTRAINT IF EXISTS feedbacks_reporter_consent_notice_snapshot_valid,
    DROP COLUMN IF EXISTS reporter_consent_notice_snapshot;

ALTER TABLE feedbacks
    ADD CONSTRAINT feedbacks_reporter_consent_evidence_consistent CHECK (
        (reporter_consent_purpose IS NULL AND reporter_consent_notice_version IS NULL
            AND reporter_consent_locale IS NULL AND reporter_consent_source IS NULL)
        OR (reporter_consent_purpose IS NOT NULL
            AND reporter_consent_notice_version IS NOT NULL
            AND reporter_consent_locale IS NOT NULL
            AND reporter_consent_source IS NOT NULL
            AND reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND lower(split_part(reporter_consent_locale, '-', 1)) IN ('en', 'pt')
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL)
    );

ALTER TABLE issues
    ADD CONSTRAINT issues_reporter_consent_evidence_consistent CHECK (
        (reporter_consent_purpose IS NULL AND reporter_consent_notice_version IS NULL
            AND reporter_consent_locale IS NULL AND reporter_consent_source IS NULL)
        OR (reporter_consent_purpose IS NOT NULL
            AND reporter_consent_notice_version IS NOT NULL
            AND reporter_consent_locale IS NOT NULL
            AND reporter_consent_source IS NOT NULL
            AND reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND lower(split_part(reporter_consent_locale, '-', 1)) IN ('en', 'pt')
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL)
    );

CREATE INDEX idx_feedbacks_reporter_consent_active
    ON feedbacks (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version = '2026-07'
      AND reporter_consent_source = 'widget_submission';

CREATE INDEX idx_issues_reporter_consent_active
    ON issues (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version = '2026-07'
      AND reporter_consent_source = 'widget_submission';

REVOKE UPDATE (
    privacy_contact_email, privacy_rights_email, privacy_profile_version
) ON tenants FROM fidibeki_app;

ALTER TABLE tenants
    DROP CONSTRAINT IF EXISTS tenants_privacy_profile_version_valid,
    DROP CONSTRAINT IF EXISTS tenants_privacy_rights_email_valid,
    DROP CONSTRAINT IF EXISTS tenants_privacy_contact_email_valid,
    DROP COLUMN IF EXISTS privacy_profile_version,
    DROP COLUMN IF EXISTS privacy_rights_email,
    DROP COLUMN IF EXISTS privacy_contact_email;
