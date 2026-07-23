CREATE TYPE member_status AS ENUM ('active', 'deactivated', 'removed');

ALTER TABLE tenant_members
    ADD COLUMN status member_status NOT NULL DEFAULT 'active',
    ADD COLUMN authorization_version integer NOT NULL DEFAULT 0
        CHECK (authorization_version >= 0);

ALTER TABLE tenants ADD COLUMN owner_member_id uuid;

UPDATE tenants AS tenant
SET owner_member_id = (
    SELECT member.id
    FROM tenant_members AS member
    WHERE member.tenant_id = tenant.id
    ORDER BY member.created_at, member.id
    LIMIT 1
)
WHERE EXISTS (
    SELECT 1 FROM tenant_members AS member WHERE member.tenant_id = tenant.id
);

UPDATE tenant_members AS member
SET permissions = ARRAY[
    'project:create', 'project:read', 'project:update', 'project:delete',
    'widget_rule:create', 'widget_rule:read', 'widget_rule:update', 'widget_rule:delete',
    'feedback:read', 'feedback:update', 'feedback:delete',
    'issue:read', 'issue:update', 'issue:delete', 'issue:merge',
    'session:read', 'session:delete',
    'member:read', 'member:invite', 'member:update', 'member:remove',
    'api_key:create', 'api_key:read', 'api_key:delete',
    'integration:create', 'integration:read', 'integration:update', 'integration:delete',
    'billing:manage', 'billing:read', 'tenant:update', 'tenant:read'
]::text[]
FROM tenants AS tenant
WHERE tenant.id = member.tenant_id AND tenant.owner_member_id = member.id;

ALTER TABLE tenants
    ADD CONSTRAINT tenants_owner_member_fkey
    FOREIGN KEY (id, owner_member_id)
    REFERENCES tenant_members(tenant_id, id)
    ON DELETE SET NULL (owner_member_id);

CREATE TABLE tenant_access_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    actor_member_id uuid,
    actor_platform_admin_id uuid REFERENCES platform_admins(id) ON DELETE SET NULL,
    subject_member_id uuid,
    data jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_access_events_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT tenant_access_events_actor_fkey
        FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id),
    CONSTRAINT tenant_access_events_subject_fkey
        FOREIGN KEY (tenant_id, subject_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (subject_member_id)
);

CREATE INDEX idx_tenant_access_events_created
    ON tenant_access_events (tenant_id, created_at DESC, id DESC);

ALTER TABLE tenant_access_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_access_events FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_access_events_tenant_select ON tenant_access_events
    FOR SELECT TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_access_events_tenant_insert ON tenant_access_events
    FOR INSERT TO fidibeki_app
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE member_status TO fidibeki_app, fidibeki_admin;
GRANT UPDATE (owner_member_id) ON tenants TO fidibeki_app;
GRANT SELECT, INSERT ON tenant_access_events TO fidibeki_app, fidibeki_admin;
