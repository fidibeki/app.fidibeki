ALTER TYPE event_type
    ADD VALUE IF NOT EXISTS 'email_feedback.invitation_created';

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN (
        'feedback', 'issue', 'tenant',
        'privacy_recipient_action', 'privacy_case_communication',
        'email_feedback_invitation'
    ));

CREATE TYPE email_feedback_invitation_status AS ENUM (
    'queued', 'sent', 'submitted', 'suppressed',
    'bounced', 'complained', 'failed', 'expired'
);

CREATE TYPE email_feedback_suppression_reason AS ENUM (
    'opt_out', 'bounce', 'complaint'
);

ALTER TABLE email_feedback_campaigns
    ADD CONSTRAINT email_feedback_campaigns_tenant_project_id_key
    UNIQUE (tenant_id, project_id, id);

CREATE TABLE email_feedback_suppressions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recipient_digest text NOT NULL CHECK (
        recipient_digest ~ '^[0-9a-f]{64}$'
    ),
    reason email_feedback_suppression_reason NOT NULL,
    source_invitation_id uuid,
    first_suppressed_at timestamptz NOT NULL,
    last_suppressed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT email_feedback_suppressions_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT email_feedback_suppressions_recipient_key
        UNIQUE (tenant_id, recipient_digest),
    CONSTRAINT email_feedback_suppressions_timestamps_consistent
        CHECK (last_suppressed_at >= first_suppressed_at)
);

CREATE TABLE email_feedback_invitations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    widget_rule_id uuid NOT NULL,
    issuer_api_key_id uuid,
    external_event_id text NOT NULL CHECK (
        char_length(external_event_id) BETWEEN 1 AND 200
        AND external_event_id !~ '[[:cntrl:]]'
    ),
    request_digest text NOT NULL CHECK (
        request_digest ~ '^[0-9a-f]{64}$'
    ),
    recipient_email citext CHECK (
        recipient_email IS NULL
        OR char_length(recipient_email::text) BETWEEN 3 AND 320
    ),
    recipient_digest text NOT NULL CHECK (
        recipient_digest ~ '^[0-9a-f]{64}$'
    ),
    locale text NOT NULL CHECK (
        char_length(locale) BETWEEN 2 AND 35
        AND locale ~ '^[a-z]{2,3}(-[a-z0-9]{2,8})*$'
    ),
    context_url text CHECK (
        context_url IS NULL
        OR (
            char_length(context_url) BETWEEN 9 AND 2048
            AND context_url ~ '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
        )
    ),
    token_hash text CHECK (
        token_hash IS NULL OR token_hash ~ '^[0-9a-f]{64}$'
    ),
    definition_version integer NOT NULL CHECK (definition_version >= 1),
    controller_profile_version integer NOT NULL CHECK (
        controller_profile_version >= 0
    ),
    campaign_snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(campaign_snapshot) = 'object'
    ),
    definition_snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(definition_snapshot) = 'object'
    ),
    localization_snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(localization_snapshot) = 'object'
    ),
    controller_snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(controller_snapshot) = 'object'
    ),
    status email_feedback_invitation_status NOT NULL DEFAULT 'queued',
    provider_message_id text CHECK (
        provider_message_id IS NULL
        OR char_length(provider_message_id) BETWEEN 1 AND 500
    ),
    feedback_id uuid,
    sent_at timestamptz,
    submitted_at timestamptz,
    suppressed_at timestamptz,
    bounced_at timestamptz,
    complained_at timestamptz,
    failed_at timestamptz,
    expired_at timestamptz,
    last_error text CHECK (
        last_error IS NULL OR char_length(last_error) BETWEEN 1 AND 2000
    ),
    expires_at timestamptz NOT NULL,
    cleanup_after timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT email_feedback_invitations_tenant_project_campaign_fkey
        FOREIGN KEY (tenant_id, project_id, campaign_id)
        REFERENCES email_feedback_campaigns(tenant_id, project_id, id)
        ON DELETE CASCADE,
    CONSTRAINT email_feedback_invitations_project_rule_fkey
        FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT email_feedback_invitations_tenant_issuer_fkey
        FOREIGN KEY (tenant_id, issuer_api_key_id)
        REFERENCES api_keys(tenant_id, id) ON DELETE SET NULL (issuer_api_key_id),
    CONSTRAINT email_feedback_invitations_tenant_feedback_fkey
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE SET NULL (feedback_id),
    CONSTRAINT email_feedback_invitations_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT email_feedback_invitations_event_key
        UNIQUE (tenant_id, campaign_id, external_event_id),
    CONSTRAINT email_feedback_invitations_deadline_valid
        CHECK (
            expires_at > created_at
            AND (cleanup_after IS NULL OR cleanup_after >= expires_at)
        ),
    CONSTRAINT email_feedback_invitations_initial_state_consistent
        CHECK (
            (
                status = 'queued'
                AND recipient_email IS NOT NULL
                AND token_hash IS NOT NULL
            )
            OR (
                status = 'suppressed'
                AND recipient_email IS NULL
                AND suppressed_at IS NOT NULL
            )
            OR status NOT IN ('queued', 'suppressed')
        )
);

ALTER TABLE email_feedback_suppressions
    ADD CONSTRAINT email_feedback_suppressions_tenant_invitation_fkey
    FOREIGN KEY (tenant_id, source_invitation_id)
    REFERENCES email_feedback_invitations(tenant_id, id)
    ON DELETE SET NULL (source_invitation_id);

CREATE INDEX idx_email_feedback_invitations_campaign
    ON email_feedback_invitations (
        tenant_id, campaign_id, created_at DESC, id DESC
    );
CREATE INDEX idx_email_feedback_invitations_recipient
    ON email_feedback_invitations (tenant_id, recipient_digest, created_at DESC);
CREATE INDEX idx_email_feedback_invitations_status
    ON email_feedback_invitations (status, created_at, id);
CREATE UNIQUE INDEX idx_email_feedback_invitations_token
    ON email_feedback_invitations (token_hash)
    WHERE token_hash IS NOT NULL;
CREATE INDEX idx_email_feedback_suppressions_reason
    ON email_feedback_suppressions (tenant_id, reason, last_suppressed_at DESC);

CREATE TRIGGER email_feedback_invitations_set_updated_at
BEFORE UPDATE ON email_feedback_invitations
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER email_feedback_suppressions_set_updated_at
BEFORE UPDATE ON email_feedback_suppressions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE email_feedback_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_feedback_invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY email_feedback_invitations_tenant_isolation
    ON email_feedback_invitations
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE email_feedback_suppressions ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_feedback_suppressions FORCE ROW LEVEL SECURITY;
CREATE POLICY email_feedback_suppressions_tenant_isolation
    ON email_feedback_suppressions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE email_feedback_invitation_status,
    email_feedback_suppression_reason TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON email_feedback_invitations, email_feedback_suppressions
    TO fidibeki_app, fidibeki_admin;

-- Existing owners and administrative members must be able to delegate the new
-- server-to-server permission to a least-privilege API key.
UPDATE tenant_members AS member
SET permissions = member.permissions || ARRAY['email_feedback:send']::text[],
    authorization_version = member.authorization_version + 1
FROM tenants AS tenant
WHERE tenant.id = member.tenant_id
  AND member.status = 'active'
  AND NOT ('email_feedback:send' = ANY(member.permissions))
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
