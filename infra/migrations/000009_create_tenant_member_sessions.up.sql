CREATE TABLE tenant_member_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    member_id uuid NOT NULL,
    refresh_hash text NOT NULL UNIQUE CHECK (char_length(refresh_hash) = 64),
    issued_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    user_agent text,
    ip inet,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_member_sessions_member_fk
        FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX idx_tenant_member_sessions_member_id ON tenant_member_sessions (member_id);

ALTER TABLE tenant_member_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_member_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_member_sessions_tenant_isolation ON tenant_member_sessions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_member_sessions TO fidibeki_app, fidibeki_admin;
