CREATE TYPE feedback_source AS ENUM ('web', 'ios', 'android');

ALTER TABLE feedbacks
    ADD COLUMN source feedback_source NOT NULL DEFAULT 'web',
    ADD COLUMN mobile_application_id text,
    ADD COLUMN mobile_screen text,
    ADD COLUMN mobile_app_version text,
    ADD COLUMN mobile_os_version text;

ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_page_url_length,
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
    );

CREATE TABLE mobile_capture_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    widget_rule_id uuid NOT NULL,
    issuer_api_key_id uuid NOT NULL,
    token_hash text NOT NULL UNIQUE CHECK (
        token_hash ~ '^[0-9a-f]{64}$'
    ),
    platform feedback_source NOT NULL CHECK (platform IN ('ios', 'android')),
    application_id text NOT NULL CHECK (
        char_length(application_id) BETWEEN 3 AND 255
        AND application_id ~
            '^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_-]*)+$'
    ),
    screen text NOT NULL CHECK (
        char_length(screen) BETWEEN 1 AND 512
        AND screen ~ '^/[A-Za-z0-9._~!$&''()*+,;=:@%/-]*$'
        AND screen !~ '[?#]'
    ),
    context_url text CHECK (
        context_url IS NULL
        OR (
            char_length(context_url) BETWEEN 9 AND 4096
            AND context_url ~ '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
        )
    ),
    display_locale text NOT NULL CHECK (
        char_length(display_locale) BETWEEN 2 AND 35
        AND display_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    ),
    definition_version integer NOT NULL CHECK (definition_version >= 1),
    controller_profile_version integer NOT NULL CHECK (controller_profile_version >= 0),
    feedback_id uuid,
    client_feedback_id uuid,
    request_digest text CHECK (
        request_digest IS NULL OR request_digest ~ '^[0-9a-f]{64}$'
    ),
    accepted_at timestamptz,
    submit_expires_at timestamptz NOT NULL,
    retry_expires_at timestamptz,
    cleanup_after timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT mobile_capture_sessions_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT mobile_capture_sessions_tenant_issuer_fkey
        FOREIGN KEY (tenant_id, issuer_api_key_id)
        REFERENCES api_keys(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT mobile_capture_sessions_project_rule_fkey
        FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE CASCADE,
    CONSTRAINT mobile_capture_sessions_tenant_feedback_fkey
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT mobile_capture_sessions_binding_consistent CHECK (
        (
            feedback_id IS NULL
            AND client_feedback_id IS NULL
            AND request_digest IS NULL
            AND accepted_at IS NULL
            AND retry_expires_at IS NULL
        )
        OR (
            feedback_id IS NOT NULL
            AND client_feedback_id IS NOT NULL
            AND request_digest IS NOT NULL
            AND accepted_at IS NOT NULL
            AND retry_expires_at IS NOT NULL
            AND retry_expires_at > accepted_at
        )
    ),
    CONSTRAINT mobile_capture_sessions_deadlines_consistent CHECK (
        submit_expires_at > created_at
        AND cleanup_after >= submit_expires_at
        AND (retry_expires_at IS NULL OR cleanup_after >= retry_expires_at)
        AND cleanup_after <=
            GREATEST(submit_expires_at, COALESCE(retry_expires_at, submit_expires_at))
            + interval '24 hours'
    ),
    CONSTRAINT mobile_capture_sessions_tenant_id_id_key UNIQUE (tenant_id, id)
);

CREATE INDEX idx_mobile_capture_sessions_cleanup
    ON mobile_capture_sessions (cleanup_after, id);
CREATE INDEX idx_mobile_capture_sessions_issuer
    ON mobile_capture_sessions (issuer_api_key_id);
CREATE INDEX idx_mobile_capture_sessions_feedback
    ON mobile_capture_sessions (tenant_id, feedback_id)
    WHERE feedback_id IS NOT NULL;

CREATE TRIGGER mobile_capture_sessions_set_updated_at
BEFORE UPDATE ON mobile_capture_sessions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE mobile_capture_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_capture_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY mobile_capture_sessions_tenant_isolation ON mobile_capture_sessions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE feedback_source TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON mobile_capture_sessions TO fidibeki_app, fidibeki_admin;

-- This grant is intentionally narrower than "all active members". Existing
-- owners and administrative members already able to create API keys and read
-- both projects and widget definitions can mint the least-privilege key.
UPDATE tenant_members AS member
SET permissions = member.permissions || ARRAY['mobile_capture:create']::text[],
    authorization_version = member.authorization_version + 1
FROM tenants AS tenant
WHERE tenant.id = member.tenant_id
  AND member.status = 'active'
  AND NOT ('mobile_capture:create' = ANY(member.permissions))
  AND 'api_key:create' = ANY(member.permissions)
  AND 'project:read' = ANY(member.permissions)
  AND 'widget_rule:read' = ANY(member.permissions)
  AND (
      tenant.owner_member_id = member.id
      OR (
          'member:read' = ANY(member.permissions)
          AND 'tenant:read' = ANY(member.permissions)
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
            AND reporter_consent_source IN ('widget_submission', 'mobile_submission')
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
            AND reporter_consent_source IN ('widget_submission', 'mobile_submission')
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
      AND reporter_consent_source IN ('widget_submission', 'mobile_submission');

CREATE INDEX idx_issues_reporter_consent_active
    ON issues (tenant_id, id)
    WHERE notify_reporter = true
      AND reporter_email IS NOT NULL
      AND reporter_consent_granted_at IS NOT NULL
      AND reporter_consent_withdrawn_at IS NULL
      AND reporter_consent_purpose = 'feedback_updates'
      AND reporter_consent_notice_version IN ('2026-07', '2026-07-controller-v1')
      AND reporter_consent_source IN ('widget_submission', 'mobile_submission');
