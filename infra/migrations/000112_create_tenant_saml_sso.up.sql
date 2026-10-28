-- Organization-scoped SAML 2.0 SSO. Disabled by
-- default: a tenant without a tenant_saml_configurations row behaves exactly as
-- before. SAML authenticates identity only; Fidibeki permissions remain the
-- authorization source, so nothing here grants a role.

CREATE TYPE tenant_sso_mode AS ENUM ('disabled', 'optional', 'required');

CREATE TABLE tenant_saml_configurations (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    mode tenant_sso_mode NOT NULL DEFAULT 'disabled',
    idp_entity_id text NOT NULL CHECK (char_length(idp_entity_id) BETWEEN 1 AND 1024),
    idp_sso_url text NOT NULL CHECK (char_length(idp_sso_url) BETWEEN 1 AND 2048),
    idp_metadata_url text CHECK (idp_metadata_url IS NULL OR char_length(idp_metadata_url) BETWEEN 1 AND 2048),
    -- Email domains the IdP is trusted to assert for. Empty means "any domain
    -- an existing member already uses"; membership is always still required.
    email_domains text[] NOT NULL DEFAULT '{}',
    allow_idp_initiated boolean NOT NULL DEFAULT false,
    -- SP key pair used to sign AuthnRequests and decrypt EncryptedAssertions.
    -- The private key is AES-256-GCM sealed under INTEGRATION_ENCRYPTION_KEY
    -- with the tenant ID as associated data; it never leaves the API.
    sp_private_key bytea NOT NULL,
    sp_certificate text NOT NULL,
    -- A successful test assertion is the precondition for Required mode. Any
    -- change to the IdP identity clears it, so Required can never be enabled
    -- against an untested configuration.
    last_tested_at timestamptz,
    last_test_error text,
    last_successful_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by_member_id uuid,
    CONSTRAINT tenant_saml_configurations_updated_by_fkey
        FOREIGN KEY (tenant_id, updated_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (updated_by_member_id)
);

CREATE TRIGGER tenant_saml_configurations_set_updated_at
BEFORE UPDATE ON tenant_saml_configurations
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Several signing certificates can be active at once so an IdP can publish its
-- next certificate before retiring the current one: rotation without an
-- authentication outage.
CREATE TABLE tenant_saml_certificates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenant_saml_configurations(tenant_id) ON DELETE CASCADE,
    certificate text NOT NULL CHECK (char_length(certificate) BETWEEN 1 AND 16384),
    fingerprint text NOT NULL CHECK (char_length(fingerprint) = 64),
    subject text NOT NULL DEFAULT '',
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_saml_certificates_tenant_fingerprint_key UNIQUE (tenant_id, fingerprint),
    CONSTRAINT tenant_saml_certificates_validity_ordered CHECK (not_after > not_before)
);

CREATE INDEX idx_tenant_saml_certificates_tenant
    ON tenant_saml_certificates (tenant_id, not_after DESC);

-- Correlates a SAML Response back to the AuthnRequest this SP issued. A
-- response whose InResponseTo matches no unconsumed row here is rejected, which
-- is what keeps IdP-initiated support from becoming a forgery window.
CREATE TABLE tenant_saml_auth_requests (
    id text NOT NULL CHECK (char_length(id) BETWEEN 1 AND 128),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('login', 'test')),
    relay_state text NOT NULL DEFAULT '' CHECK (char_length(relay_state) <= 512),
    return_to text NOT NULL DEFAULT '' CHECK (char_length(return_to) <= 512),
    actor_member_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT tenant_saml_auth_requests_expiry_valid CHECK (expires_at > created_at),
    CONSTRAINT tenant_saml_auth_requests_actor_fkey
        FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id)
);

CREATE INDEX idx_tenant_saml_auth_requests_expiry
    ON tenant_saml_auth_requests (expires_at);

-- Assertion replay ledger. Rows are retained until the assertion could no
-- longer be accepted on timestamp grounds, then swept by the maintenance job.
CREATE TABLE tenant_saml_assertions (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    assertion_hash text NOT NULL CHECK (char_length(assertion_hash) = 64),
    not_on_or_after timestamptz NOT NULL,
    seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, assertion_hash)
);

CREATE INDEX idx_tenant_saml_assertions_expiry
    ON tenant_saml_assertions (not_on_or_after);

-- Migration 000111 already records authentication_method on a session for
-- two-factor purposes. These columns add which identity provider asserted it
-- and under what subject, which is what makes a SAML session auditable.
ALTER TABLE tenant_member_sessions
    ADD COLUMN saml_idp_entity_id text,
    ADD COLUMN saml_subject_id text,
    ADD CONSTRAINT tenant_member_sessions_saml_identity_consistency CHECK (
        (authentication_method = 'saml' AND saml_idp_entity_id IS NOT NULL AND saml_subject_id IS NOT NULL)
        OR (authentication_method <> 'saml' AND saml_idp_entity_id IS NULL AND saml_subject_id IS NULL)
    );

-- The ACS hands off through a short-lived single-use magic link so SAML reuses
-- the proven session-issuing path. These columns carry the authentication
-- provenance across that handoff and onto the session.
ALTER TABLE tenant_magic_links
    ADD COLUMN authentication_method text NOT NULL DEFAULT 'local'
        CHECK (authentication_method IN ('local', 'saml')),
    ADD COLUMN saml_idp_entity_id text,
    ADD COLUMN saml_subject_id text,
    ADD CONSTRAINT tenant_magic_links_saml_identity_consistency CHECK (
        (authentication_method = 'saml' AND saml_idp_entity_id IS NOT NULL AND saml_subject_id IS NOT NULL)
        OR (authentication_method = 'local' AND saml_idp_entity_id IS NULL AND saml_subject_id IS NULL)
    );

ALTER TABLE tenant_saml_configurations ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_saml_configurations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_saml_configurations_tenant_isolation ON tenant_saml_configurations
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_saml_certificates ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_saml_certificates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_saml_certificates_tenant_isolation ON tenant_saml_certificates
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_saml_auth_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_saml_auth_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_saml_auth_requests_tenant_isolation ON tenant_saml_auth_requests
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_saml_assertions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_saml_assertions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_saml_assertions_tenant_isolation ON tenant_saml_assertions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Correlation and replay rows are ephemeral authentication credentials with the
-- same lifecycle as magic links: authoritative expiry, hard delete, no content
-- retained. They join the existing bounded retention-health ledger so the sweep
-- is observable rather than silent.
ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    ADD CONSTRAINT retention_maintenance_status_operation_key_check
        CHECK (operation_key IN (
            'tenant_magic_links',
            'platform_admin_magic_links',
            'attachments',
            'privacy_export_archives',
            'reporter_privacy_requests',
            'replay_sessions',
            'widget_funnel_events',
            'widget_funnel_daily',
            'abuse_diagnostics',
            'ai_budget_ledgers',
            'identity_asset_deletions',
            'saml_auth_artifacts'
        ));

INSERT INTO retention_maintenance_status (
    operation_key, category, expected_interval_seconds, alert_grace_seconds
) VALUES ('saml_auth_artifacts', 'auth_magic_links', 3600, 900);

GRANT USAGE ON TYPE tenant_sso_mode TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_saml_configurations TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_saml_certificates TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_saml_auth_requests TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_saml_assertions TO fidibeki_app, fidibeki_admin;
