CREATE TYPE tenant_plan AS ENUM ('starter', 'growth', 'scale', 'enterprise');
CREATE TYPE tenant_status AS ENUM ('active', 'suspended', 'trial', 'cancelled');

CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    slug text NOT NULL UNIQUE,
    plan tenant_plan NOT NULL DEFAULT 'starter',
    status tenant_status NOT NULL DEFAULT 'trial',
    billing_email citext,
    trial_ends_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenants_slug_valid CHECK (
        slug ~ '^[a-z0-9][a-z0-9-]{1,22}[a-z0-9]$'
        AND slug NOT IN (
            'api', 'www', 'admin', 'mail', 'widget', 'auth', 'billing', 'app',
            'staging', 'dev', 'test', 'demo', 'support', 'help'
        )
    ),
    CONSTRAINT tenants_trial_end_present CHECK (
        status <> 'trial' OR trial_ends_at IS NOT NULL
    )
);

CREATE INDEX idx_tenants_status ON tenants (status);

CREATE TRIGGER tenants_set_updated_at
BEFORE UPDATE ON tenants
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;

CREATE POLICY tenants_select_self ON tenants
    FOR SELECT TO fidibeki_app
    USING (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY tenants_update_self ON tenants
    FOR UPDATE TO fidibeki_app
    USING (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE tenant_plan, tenant_status TO fidibeki_app, fidibeki_admin;
GRANT SELECT ON tenants TO fidibeki_app;
GRANT UPDATE (name, billing_email) ON tenants TO fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants TO fidibeki_admin;
