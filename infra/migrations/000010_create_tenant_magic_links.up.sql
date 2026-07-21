CREATE TABLE tenant_magic_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    member_id uuid,
    email citext NOT NULL,
    token_hash text NOT NULL UNIQUE CHECK (char_length(token_hash) = 64),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_magic_links_member_fk
        FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT tenant_magic_links_expiry_valid CHECK (expires_at > created_at),
    CONSTRAINT tenant_magic_links_email_valid CHECK (email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);

CREATE INDEX idx_tenant_magic_links_email ON tenant_magic_links (email);

ALTER TABLE tenant_magic_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_magic_links FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_magic_links_tenant_isolation ON tenant_magic_links
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_magic_links TO fidibeki_app, fidibeki_admin;
