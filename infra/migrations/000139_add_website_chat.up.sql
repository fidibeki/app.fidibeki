ALTER TABLE projects ADD COLUMN chat_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE website_chat_visitors (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    origin text NOT NULL,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash)=32),
    expires_at timestamptz NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence>=0),
    visitor_read_sequence bigint NOT NULL DEFAULT 0 CHECK (visitor_read_sequence>=0),
    operator_read_sequence bigint NOT NULL DEFAULT 0 CHECK (operator_read_sequence>=0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,project_id,id),
    FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id) ON DELETE CASCADE,
    CHECK (visitor_read_sequence<=last_sequence AND operator_read_sequence<=last_sequence)
);
CREATE INDEX website_chat_inbox ON website_chat_visitors(tenant_id,project_id,updated_at DESC,id DESC) WHERE last_sequence>0;
CREATE INDEX website_chat_expiry ON website_chat_visitors(expires_at) WHERE last_sequence=0;
CREATE TABLE website_chat_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    visitor_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence>0),
    client_id uuid NOT NULL,
    sender_type text NOT NULL CHECK (sender_type IN ('operator','visitor')),
    sender_id uuid NOT NULL,
    sender_name text NOT NULL CHECK (char_length(sender_name) BETWEEN 1 AND 120),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 4000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id,project_id,visitor_id) REFERENCES website_chat_visitors(tenant_id,project_id,id) ON DELETE CASCADE,
    UNIQUE(visitor_id,sequence),
    UNIQUE(visitor_id,sender_type,sender_id,client_id),
    CHECK (sender_type<>'visitor' OR sender_id=visitor_id)
);
CREATE INDEX website_chat_unread ON website_chat_messages(visitor_id,sender_type,sequence);
ALTER TABLE website_chat_visitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE website_chat_visitors FORCE ROW LEVEL SECURITY;
ALTER TABLE website_chat_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE website_chat_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY website_chat_visitors_scope ON website_chat_visitors FOR ALL TO fidibeki_app
 USING (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
   AND project_id=NULLIF(current_setting('app.chat_project_id',true),'')::uuid
   AND (current_setting('app.chat_operator',true)='true' OR id=NULLIF(current_setting('app.chat_visitor_id',true),'')::uuid))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
   AND project_id=NULLIF(current_setting('app.chat_project_id',true),'')::uuid
   AND (current_setting('app.chat_operator',true)='true' OR id=NULLIF(current_setting('app.chat_visitor_id',true),'')::uuid));
CREATE POLICY website_chat_messages_scope ON website_chat_messages FOR ALL TO fidibeki_app
 USING (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
   AND project_id=NULLIF(current_setting('app.chat_project_id',true),'')::uuid
   AND (current_setting('app.chat_operator',true)='true' OR visitor_id=NULLIF(current_setting('app.chat_visitor_id',true),'')::uuid))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
   AND project_id=NULLIF(current_setting('app.chat_project_id',true),'')::uuid
   AND (current_setting('app.chat_operator',true)='true' OR visitor_id=NULLIF(current_setting('app.chat_visitor_id',true),'')::uuid));
GRANT SELECT,INSERT,UPDATE,DELETE ON website_chat_visitors TO fidibeki_app,fidibeki_admin;
GRANT SELECT,INSERT,DELETE ON website_chat_messages TO fidibeki_app,fidibeki_admin;

-- Existing workspace administrators inherit the tenant product capability.
-- Custom roles receive no new permission implicitly.
UPDATE tenant_members SET permissions=permissions||ARRAY['chat:read','chat:write','chat:delete']::text[],
 authorization_version=authorization_version+1
 WHERE permissions @> ARRAY['project:create','project:read','project:update','project:delete','widget_rule:create','widget_rule:read','widget_rule:update','widget_rule:delete','feedback:read','feedback:update','feedback:delete','issue:read','issue:update','issue:delete','issue:merge','session:read','session:delete','member:read','member:invite','member:update','member:remove','api_key:create','api_key:read','api_key:delete','mobile_capture:create','email_feedback:send','integration:create','integration:read','integration:update','integration:delete','testimonial:read','testimonial:create','testimonial:update','testimonial:delete','testimonial:publish','billing:manage','billing:read','tenant:update','tenant:read','audit:read']::text[];

CREATE INDEX website_chat_retention ON website_chat_visitors(updated_at) WHERE last_sequence>0;
ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    DROP CONSTRAINT retention_maintenance_status_category_check,
    ADD CONSTRAINT retention_maintenance_status_operation_key_check CHECK (operation_key IN (
        'tenant_magic_links', 'platform_admin_magic_links', 'attachments',
        'privacy_export_archives', 'reporter_privacy_requests', 'replay_sessions',
        'widget_funnel_events', 'widget_funnel_daily', 'abuse_diagnostics',
        'ai_budget_ledgers', 'identity_asset_deletions', 'saml_auth_artifacts',
        'mobile_capture_sessions', 'email_feedback_invitations', 'support_conversations', 'website_chat_conversations'
    )),
    ADD CONSTRAINT retention_maintenance_status_category_check CHECK (category IN (
        'auth_magic_links', 'attachments', 'export_archives', 'privacy_rights',
        'session_replays', 'widget_events', 'widget_daily', 'abuse_diagnostics',
        'ai_processing', 'identity_assets', 'mobile_capture_credentials',
        'email_feedback_delivery', 'platform_support', 'website_chat'
    ));
INSERT INTO retention_maintenance_status(operation_key,category,expected_interval_seconds,alert_grace_seconds)
VALUES ('website_chat_conversations','website_chat',86400,900);
