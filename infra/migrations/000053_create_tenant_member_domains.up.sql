CREATE TABLE tenant_member_domains (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain citext NOT NULL,
    verification_email citext NOT NULL,
    verification_token_hash text NOT NULL UNIQUE CHECK (char_length(verification_token_hash) = 64),
    verification_expires_at timestamptz NOT NULL,
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_member_domains_tenant_domain_key UNIQUE (tenant_id, domain),
    CONSTRAINT tenant_member_domains_domain_valid CHECK (
        char_length(domain::text) BETWEEN 3 AND 253
        AND domain::text ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'
    ),
    CONSTRAINT tenant_member_domains_email_valid CHECK (
        verification_email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
    ),
    CONSTRAINT tenant_member_domains_expiry_valid CHECK (verification_expires_at > created_at)
);

CREATE INDEX idx_tenant_member_domains_verified
    ON tenant_member_domains (tenant_id, domain)
    WHERE verified_at IS NOT NULL;

CREATE TRIGGER tenant_member_domains_set_updated_at
BEFORE UPDATE ON tenant_member_domains
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tenant_member_domains ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_member_domains FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_member_domains_tenant_isolation ON tenant_member_domains
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_member_domains TO fidibeki_app, fidibeki_admin;
