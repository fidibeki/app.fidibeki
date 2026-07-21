CREATE TABLE tenant_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email citext NOT NULL,
    name text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 120),
    permissions text[] NOT NULL DEFAULT '{}',
    email_bounced boolean NOT NULL DEFAULT false,
    email_complained boolean NOT NULL DEFAULT false,
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_members_tenant_email_key UNIQUE (tenant_id, email),
    CONSTRAINT tenant_members_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT tenant_members_email_valid CHECK (email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);

CREATE INDEX idx_tenant_members_email ON tenant_members (email);

CREATE TRIGGER tenant_members_set_updated_at
BEFORE UPDATE ON tenant_members
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_members FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_members_tenant_isolation ON tenant_members
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_members TO fidibeki_app, fidibeki_admin;
