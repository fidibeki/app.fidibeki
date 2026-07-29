ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_source_context_consistent;
ALTER TABLE mobile_capture_sessions
    DROP CONSTRAINT mobile_capture_sessions_platform_check;

ALTER TABLE feedbacks ALTER COLUMN source DROP DEFAULT;
ALTER TABLE feedbacks
    ALTER COLUMN source TYPE text USING source::text;
ALTER TABLE mobile_capture_sessions
    ALTER COLUMN platform TYPE text USING platform::text;

DROP TYPE feedback_source;
CREATE TYPE feedback_source AS ENUM ('web', 'ios', 'android', 'email');

ALTER TABLE feedbacks
    ALTER COLUMN source TYPE feedback_source USING source::feedback_source,
    ALTER COLUMN source SET DEFAULT 'web';
ALTER TABLE mobile_capture_sessions
    ALTER COLUMN platform TYPE feedback_source USING platform::feedback_source,
    ADD CONSTRAINT mobile_capture_sessions_platform_check
        CHECK (platform IN ('ios', 'android'));

GRANT USAGE ON TYPE feedback_source TO fidibeki_app, fidibeki_admin;

ALTER TABLE feedbacks
    ADD CONSTRAINT feedbacks_source_context_consistent CHECK (
        (
            source = 'web'
            AND char_length(page_url) BETWEEN 1 AND 4096
            AND page_url ~ '^https?://'
            AND mobile_application_id IS NULL
            AND mobile_screen IS NULL
            AND mobile_app_version IS NULL
            AND mobile_os_version IS NULL
        )
        OR (
            source IN ('ios', 'android')
            AND (
                page_url = ''
                OR (
                    char_length(page_url) BETWEEN 9 AND 4096
                    AND page_url ~ '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
                )
            )
            AND mobile_application_id IS NOT NULL
            AND mobile_application_id ~
                '^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_-]*)+$'
            AND char_length(mobile_application_id) BETWEEN 3 AND 255
            AND mobile_screen IS NOT NULL
            AND mobile_screen ~ '^/[A-Za-z0-9._~!$&''()*+,;=:@%/-]*$'
            AND char_length(mobile_screen) BETWEEN 1 AND 512
            AND mobile_screen !~ '[?#]'
            AND (
                mobile_app_version IS NULL
                OR char_length(mobile_app_version) BETWEEN 1 AND 64
            )
            AND (
                mobile_os_version IS NULL
                OR char_length(mobile_os_version) BETWEEN 1 AND 64
            )
        )
        OR (
            source = 'email'
            AND (
                page_url = ''
                OR (
                    char_length(page_url) BETWEEN 9 AND 4096
                    AND page_url ~ '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
                )
            )
            AND referrer IS NULL
            AND user_agent IS NULL
            AND os IS NULL
            AND browser IS NULL
            AND device IS NULL
            AND viewport_width IS NULL
            AND viewport_height IS NULL
            AND language IS NULL
            AND dom_snapshot_html IS NULL
            AND dom_snapshot_styles IS NULL
            AND session_id IS NULL
            AND mobile_application_id IS NULL
            AND mobile_screen IS NULL
            AND mobile_app_version IS NULL
            AND mobile_os_version IS NULL
        )
    );

DROP INDEX idx_feedbacks_reporter_consent_active;
DROP INDEX idx_issues_reporter_consent_active;

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
            AND reporter_consent_source IN (
                'widget_submission', 'mobile_submission',
                'email_feedback_submission'
            )
            AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_notice_snapshot IS NOT NULL
            AND reporter_consent_notice_snapshot->>'notice_version' =
                reporter_consent_notice_version
            AND reporter_consent_notice_snapshot->>'locale' = reporter_consent_locale
        )
    );

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
            AND reporter_consent_source IN (
                'widget_submission', 'mobile_submission',
                'email_feedback_submission'
            )
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
      AND reporter_consent_source IN (
          'widget_submission', 'mobile_submission', 'email_feedback_submission'
      );

CREATE INDEX idx_issues_reporter_consent_active
    ON issues (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version IN ('2026-07', '2026-07-controller-v1')
      AND reporter_consent_source IN (
          'widget_submission', 'mobile_submission', 'email_feedback_submission'
      );
