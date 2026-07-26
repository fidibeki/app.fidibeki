SET lock_timeout = '5s';

ALTER TABLE tenants
    ADD COLUMN two_factor_policy text NOT NULL DEFAULT 'optional'
        CHECK (two_factor_policy IN ('optional', 'required')),
    ADD COLUMN two_factor_policy_updated_at timestamptz,
    ADD COLUMN two_factor_policy_updated_by uuid;

-- Column-level grants: the application role may change the policy and nothing
-- else that was not already granted on this table.
GRANT UPDATE (
    two_factor_policy,
    two_factor_policy_updated_at,
    two_factor_policy_updated_by
) ON tenants TO fidibeki_app;

-- Enrolment secrets are stored as AES-256-GCM ciphertext produced by
-- internal/twofactor; the plaintext shared secret never reaches the database,
-- an audit row, or a log line. last_used_step stores the last accepted TOTP
-- time step so a captured code cannot be replayed inside its own window.
CREATE TABLE tenant_member_totp (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    member_id uuid NOT NULL,
    secret_ciphertext bytea NOT NULL CHECK (octet_length(secret_ciphertext) BETWEEN 16 AND 512),
    digits smallint NOT NULL DEFAULT 6 CHECK (digits = 6),
    period_seconds smallint NOT NULL DEFAULT 30 CHECK (period_seconds = 30),
    confirmed_at timestamptz,
    last_used_step bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_member_totp_member_key UNIQUE (tenant_id, member_id),
    CONSTRAINT tenant_member_totp_member_fk
        FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE CASCADE
);

CREATE TRIGGER tenant_member_totp_set_updated_at
BEFORE UPDATE ON tenant_member_totp
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tenant_member_totp ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_member_totp FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_member_totp_tenant_isolation ON tenant_member_totp
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_member_totp TO fidibeki_app, fidibeki_admin;

-- Recovery codes are single-use and stored only as SHA-256 hashes, so a
-- database read can never reconstruct a usable code.
CREATE TABLE tenant_member_recovery_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    member_id uuid NOT NULL,
    code_hash text NOT NULL CHECK (char_length(code_hash) = 64),
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_member_recovery_codes_hash_key UNIQUE (tenant_id, member_id, code_hash),
    CONSTRAINT tenant_member_recovery_codes_member_fk
        FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX idx_tenant_member_recovery_codes_unused
    ON tenant_member_recovery_codes (tenant_id, member_id)
    WHERE used_at IS NULL;

ALTER TABLE tenant_member_recovery_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_member_recovery_codes FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_member_recovery_codes_tenant_isolation ON tenant_member_recovery_codes
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_member_recovery_codes TO fidibeki_app, fidibeki_admin;

-- authentication_method records how the session proved identity. SAML sessions
-- delegate their assurance to the identity provider and are never challenged by
-- Fidibeki-managed two-factor authentication.
ALTER TABLE tenant_member_sessions
    ADD COLUMN authentication_method text NOT NULL DEFAULT 'local'
        CHECK (authentication_method IN ('local', 'saml')),
    ADD COLUMN two_factor_state text NOT NULL DEFAULT 'not_required'
        CHECK (two_factor_state IN ('not_required', 'pending', 'satisfied')),
    ADD COLUMN two_factor_verified_at timestamptz;

RESET lock_timeout;
