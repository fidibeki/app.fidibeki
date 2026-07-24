ALTER TABLE tenants
    ADD COLUMN privacy_contact_email citext,
    ADD COLUMN privacy_rights_email citext,
    ADD COLUMN privacy_profile_version integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT tenants_privacy_contact_email_valid CHECK (
        privacy_contact_email IS NULL OR (
            char_length(privacy_contact_email::text) <= 320
            AND privacy_contact_email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
        )
    ),
    ADD CONSTRAINT tenants_privacy_rights_email_valid CHECK (
        privacy_rights_email IS NULL OR (
            char_length(privacy_rights_email::text) <= 320
            AND privacy_rights_email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
        )
    ),
    ADD CONSTRAINT tenants_privacy_profile_version_valid CHECK (privacy_profile_version >= 0);

GRANT UPDATE (
    privacy_contact_email, privacy_rights_email, privacy_profile_version
) ON tenants TO fidibeki_app;

ALTER TABLE feedbacks
    ADD COLUMN reporter_consent_notice_snapshot jsonb,
    ADD CONSTRAINT feedbacks_reporter_consent_notice_snapshot_valid CHECK (
        reporter_consent_notice_snapshot IS NULL OR (
            jsonb_typeof(reporter_consent_notice_snapshot) = 'object'
            AND octet_length(reporter_consent_notice_snapshot::text) <= 8192
            AND reporter_consent_notice_snapshot ?& ARRAY[
                'schema_version', 'notice_version', 'controller_profile_version',
                'widget_definition_version', 'locale', 'controller_name',
                'privacy_contact_email', 'privacy_rights_email', 'privacy_policy_url',
                'purpose_summary', 'lawful_basis_summary', 'retention_summary'
            ]::text[]
            AND (
                reporter_consent_notice_snapshot - ARRAY[
                    'schema_version', 'notice_version', 'controller_profile_version',
                    'widget_definition_version', 'locale', 'controller_name',
                    'privacy_contact_email', 'privacy_rights_email', 'privacy_policy_url',
                    'purpose_summary', 'lawful_basis_summary', 'retention_summary'
                ]::text[]
            ) = '{}'::jsonb
            AND jsonb_typeof(reporter_consent_notice_snapshot->'schema_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'schema_version')::integer = 1
            AND jsonb_typeof(reporter_consent_notice_snapshot->'controller_profile_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'controller_profile_version')::integer > 0
            AND jsonb_typeof(reporter_consent_notice_snapshot->'widget_definition_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'widget_definition_version')::integer >= 1
            AND jsonb_typeof(reporter_consent_notice_snapshot->'notice_version') = 'string'
            AND reporter_consent_notice_snapshot->>'notice_version' = '2026-07-controller-v1'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'locale') = 'string'
            AND reporter_consent_notice_snapshot->>'locale' ~
                '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'controller_name') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'controller_name') BETWEEN 1 AND 120
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_contact_email') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_contact_email') BETWEEN 3 AND 320
            AND reporter_consent_notice_snapshot->>'privacy_contact_email' ~
                '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_rights_email') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_rights_email') BETWEEN 3 AND 320
            AND reporter_consent_notice_snapshot->>'privacy_rights_email' ~
                '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_policy_url') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_policy_url') BETWEEN 9 AND 2048
            AND reporter_consent_notice_snapshot->>'privacy_policy_url' ~
                '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'purpose_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'purpose_summary') BETWEEN 1 AND 1000
            AND jsonb_typeof(reporter_consent_notice_snapshot->'lawful_basis_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'lawful_basis_summary') BETWEEN 1 AND 1000
            AND jsonb_typeof(reporter_consent_notice_snapshot->'retention_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'retention_summary') BETWEEN 1 AND 1000
        )
    );

ALTER TABLE issues
    ADD COLUMN reporter_consent_notice_snapshot jsonb,
    ADD CONSTRAINT issues_reporter_consent_notice_snapshot_valid CHECK (
        reporter_consent_notice_snapshot IS NULL OR (
            jsonb_typeof(reporter_consent_notice_snapshot) = 'object'
            AND octet_length(reporter_consent_notice_snapshot::text) <= 8192
            AND reporter_consent_notice_snapshot ?& ARRAY[
                'schema_version', 'notice_version', 'controller_profile_version',
                'widget_definition_version', 'locale', 'controller_name',
                'privacy_contact_email', 'privacy_rights_email', 'privacy_policy_url',
                'purpose_summary', 'lawful_basis_summary', 'retention_summary'
            ]::text[]
            AND (
                reporter_consent_notice_snapshot - ARRAY[
                    'schema_version', 'notice_version', 'controller_profile_version',
                    'widget_definition_version', 'locale', 'controller_name',
                    'privacy_contact_email', 'privacy_rights_email', 'privacy_policy_url',
                    'purpose_summary', 'lawful_basis_summary', 'retention_summary'
                ]::text[]
            ) = '{}'::jsonb
            AND jsonb_typeof(reporter_consent_notice_snapshot->'schema_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'schema_version')::integer = 1
            AND jsonb_typeof(reporter_consent_notice_snapshot->'controller_profile_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'controller_profile_version')::integer > 0
            AND jsonb_typeof(reporter_consent_notice_snapshot->'widget_definition_version') = 'number'
            AND (reporter_consent_notice_snapshot->>'widget_definition_version')::integer >= 1
            AND jsonb_typeof(reporter_consent_notice_snapshot->'notice_version') = 'string'
            AND reporter_consent_notice_snapshot->>'notice_version' = '2026-07-controller-v1'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'locale') = 'string'
            AND reporter_consent_notice_snapshot->>'locale' ~
                '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'controller_name') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'controller_name') BETWEEN 1 AND 120
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_contact_email') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_contact_email') BETWEEN 3 AND 320
            AND reporter_consent_notice_snapshot->>'privacy_contact_email' ~
                '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_rights_email') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_rights_email') BETWEEN 3 AND 320
            AND reporter_consent_notice_snapshot->>'privacy_rights_email' ~
                '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'privacy_policy_url') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'privacy_policy_url') BETWEEN 9 AND 2048
            AND reporter_consent_notice_snapshot->>'privacy_policy_url' ~
                '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
            AND jsonb_typeof(reporter_consent_notice_snapshot->'purpose_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'purpose_summary') BETWEEN 1 AND 1000
            AND jsonb_typeof(reporter_consent_notice_snapshot->'lawful_basis_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'lawful_basis_summary') BETWEEN 1 AND 1000
            AND jsonb_typeof(reporter_consent_notice_snapshot->'retention_summary') = 'string'
            AND char_length(reporter_consent_notice_snapshot->>'retention_summary') BETWEEN 1 AND 1000
        )
    );

DROP INDEX idx_feedbacks_reporter_consent_active;
DROP INDEX idx_issues_reporter_consent_active;

ALTER TABLE feedbacks DROP CONSTRAINT feedbacks_reporter_consent_evidence_consistent;
ALTER TABLE issues DROP CONSTRAINT issues_reporter_consent_evidence_consistent;

ALTER TABLE feedbacks
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

ALTER TABLE issues
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
