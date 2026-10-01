-- Platform permission paths have three segments, unlike tenant permissions.
ALTER TABLE tenant_access_events DROP CONSTRAINT tenant_access_events_permission_path_format;
ALTER TABLE tenant_access_events ADD CONSTRAINT tenant_access_events_permission_path_format
    CHECK (permission_path IS NULL OR permission_path ~ '^[a-z][a-z0-9_]*(:[a-z][a-z0-9_]*){1,2}$');

CREATE TABLE support_conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    member_id uuid NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    member_read_sequence bigint NOT NULL DEFAULT 0 CHECK (member_read_sequence >= 0),
    admin_read_sequence bigint NOT NULL DEFAULT 0 CHECK (admin_read_sequence >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, member_id),
    UNIQUE (tenant_id, member_id, id),
    FOREIGN KEY (tenant_id, member_id) REFERENCES tenant_members(tenant_id, id) ON DELETE CASCADE,
    CHECK (member_read_sequence <= last_sequence AND admin_read_sequence <= last_sequence)
);
CREATE INDEX support_conversations_inbox ON support_conversations (updated_at DESC, id DESC);

CREATE TABLE support_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    member_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    client_id uuid NOT NULL,
    sender_type text NOT NULL CHECK (sender_type IN ('member', 'admin')),
    sender_id uuid NOT NULL,
    sender_name text NOT NULL CHECK (char_length(sender_name) BETWEEN 1 AND 120),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 4000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, member_id, conversation_id)
        REFERENCES support_conversations(tenant_id, member_id, id) ON DELETE CASCADE,
    UNIQUE (conversation_id, sequence),
    UNIQUE (conversation_id, sender_type, sender_id, client_id),
    CHECK (sender_type <> 'member' OR sender_id = member_id)
);

ALTER TABLE support_conversations ENABLE ROW LEVEL SECURITY;
CREATE INDEX support_messages_unread ON support_messages (conversation_id, sender_type, sequence);
ALTER TABLE support_conversations FORCE ROW LEVEL SECURITY;
ALTER TABLE support_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_messages FORCE ROW LEVEL SECURITY;

CREATE POLICY support_conversations_participant ON support_conversations FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND member_id = NULLIF(current_setting('app.support_member_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND member_id = NULLIF(current_setting('app.support_member_id', true), '')::uuid);
CREATE POLICY support_messages_participant ON support_messages FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND member_id = NULLIF(current_setting('app.support_member_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND member_id = NULLIF(current_setting('app.support_member_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON support_conversations TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON support_messages TO fidibeki_app;
GRANT SELECT, INSERT, DELETE ON support_messages TO fidibeki_admin;

-- Only administrators with the complete existing root permission set inherit
-- the new authority. Other staff must be granted support access explicitly.
UPDATE platform_admins SET permissions = permissions || ARRAY['platform:support:read', 'platform:support:write']
WHERE permissions @> ARRAY[
    'platform:tenant:create', 'platform:tenant:read', 'platform:tenant:update',
    'platform:tenant:suspend', 'platform:tenant:delete', 'platform:admin:create',
    'platform:admin:read', 'platform:admin:update', 'platform:admin:delete',
    'platform:breach:create', 'platform:breach:read', 'platform:breach:append',
    'platform:queue:run', 'platform:tenant:support_access'
]::text[];

ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    DROP CONSTRAINT retention_maintenance_status_category_check,
    ADD CONSTRAINT retention_maintenance_status_operation_key_check CHECK (operation_key IN (
        'tenant_magic_links', 'platform_admin_magic_links', 'attachments',
        'privacy_export_archives', 'reporter_privacy_requests', 'replay_sessions',
        'widget_funnel_events', 'widget_funnel_daily', 'abuse_diagnostics',
        'ai_budget_ledgers', 'identity_asset_deletions', 'saml_auth_artifacts',
        'mobile_capture_sessions', 'email_feedback_invitations', 'support_conversations'
    )),
    ADD CONSTRAINT retention_maintenance_status_category_check CHECK (category IN (
        'auth_magic_links', 'attachments', 'export_archives', 'privacy_rights',
        'session_replays', 'widget_events', 'widget_daily', 'abuse_diagnostics',
        'ai_processing', 'identity_assets', 'mobile_capture_credentials',
        'email_feedback_delivery', 'platform_support'
    ));
INSERT INTO retention_maintenance_status(operation_key,category,expected_interval_seconds,alert_grace_seconds)
VALUES ('support_conversations','platform_support',86400,900);
