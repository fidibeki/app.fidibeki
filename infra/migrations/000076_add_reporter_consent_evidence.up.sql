-- Additive structured consent evidence for reporter email opt-in (issue #55).
--
-- Legacy safe policy: rows consented before this migration (reporter_consent_granted_at set via
-- migration 000062's backfill or the pre-existing trigger) have no evidence and are NOT backfilled
-- here. Application code treats a row as actively consented for delivery only when
-- reporter_consent_purpose = 'feedback_updates', so legacy rows without evidence fail closed until
-- the reporter re-confirms through a new opt-in submission or an operator-reviewed reconfirmation.
ALTER TABLE feedbacks
    ADD COLUMN reporter_consent_purpose text,
    ADD COLUMN reporter_consent_notice_version text,
    ADD COLUMN reporter_consent_locale text,
    ADD COLUMN reporter_consent_source text;

ALTER TABLE issues
    ADD COLUMN reporter_consent_purpose text,
    ADD COLUMN reporter_consent_notice_version text,
    ADD COLUMN reporter_consent_locale text,
    ADD COLUMN reporter_consent_source text;

-- Evidence fields are recorded together or not at all, and only alongside a grant timestamp. This
-- is validated for every row (no NOT VALID skip) because legacy rows already satisfy the all-null
-- branch and are never touched, so it can never break an existing row on a later, unrelated update.
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
