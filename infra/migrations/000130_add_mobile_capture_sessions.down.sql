DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM feedbacks
        WHERE source <> 'web' OR reporter_consent_source = 'mobile_submission'
        UNION ALL
        SELECT 1 FROM issues
        WHERE reporter_consent_source = 'mobile_submission'
    ) THEN
        RAISE EXCEPTION
            'cannot roll back mobile capture while mobile feedback or consent evidence exists';
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_issues_reporter_consent_active;
DROP INDEX IF EXISTS idx_feedbacks_reporter_consent_active;

ALTER TABLE issues
    DROP CONSTRAINT issues_reporter_consent_evidence_consistent,
    ADD CONSTRAINT issues_reporter_consent_evidence_consistent CHECK (
        (
            reporter_consent_purpose IS NULL
            AND reporter_consent_notice_version IS NULL
            AND reporter_consent_locale IS NULL
            AND reporter_consent_source IS NULL
            AND reporter_consent_notice_snapshot IS NULL
        )
        OR (
            reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND lower(split_part(reporter_consent_locale, '-', 1)) IN ('en', 'pt')
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_notice_snapshot IS NULL
        )
        OR (
            reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07-controller-v1'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_notice_snapshot IS NOT NULL
            AND reporter_consent_notice_snapshot->>'notice_version' =
                reporter_consent_notice_version
            AND reporter_consent_notice_snapshot->>'locale' = reporter_consent_locale
        )
    );

ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_reporter_consent_evidence_consistent,
    ADD CONSTRAINT feedbacks_reporter_consent_evidence_consistent CHECK (
        (
            reporter_consent_purpose IS NULL
            AND reporter_consent_notice_version IS NULL
            AND reporter_consent_locale IS NULL
            AND reporter_consent_source IS NULL
            AND reporter_consent_notice_snapshot IS NULL
        )
        OR (
            reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND lower(split_part(reporter_consent_locale, '-', 1)) IN ('en', 'pt')
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_notice_snapshot IS NULL
        )
        OR (
            reporter_consent_purpose = 'feedback_updates'
            AND reporter_consent_notice_version = '2026-07-controller-v1'
            AND reporter_consent_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND reporter_consent_source = 'widget_submission'
            AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_notice_snapshot IS NOT NULL
            AND reporter_consent_notice_snapshot->>'notice_version' =
                reporter_consent_notice_version
            AND reporter_consent_notice_snapshot->>'locale' = reporter_consent_locale
        )
    );

CREATE INDEX idx_feedbacks_reporter_consent_active
    ON feedbacks (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version IN ('2026-07', '2026-07-controller-v1')
      AND reporter_consent_source = 'widget_submission';

CREATE INDEX idx_issues_reporter_consent_active
    ON issues (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version IN ('2026-07', '2026-07-controller-v1')
      AND reporter_consent_source = 'widget_submission';

UPDATE tenant_members
SET permissions = array_remove(permissions, 'mobile_capture:create'),
    authorization_version = authorization_version + 1
WHERE 'mobile_capture:create' = ANY(permissions);

DROP TABLE mobile_capture_sessions;

ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_source_context_consistent,
    DROP COLUMN mobile_os_version,
    DROP COLUMN mobile_app_version,
    DROP COLUMN mobile_screen,
    DROP COLUMN mobile_application_id,
    DROP COLUMN source,
    ADD CONSTRAINT feedbacks_page_url_length CHECK (
        char_length(page_url) BETWEEN 1 AND 4096
    );

DROP TYPE feedback_source;
