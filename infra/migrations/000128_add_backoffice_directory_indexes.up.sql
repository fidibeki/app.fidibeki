-- Keep platform directory filtering/keyset scans predictable as workspace
-- counts grow. Search uses trigram indexes because the API deliberately
-- supports substring matching for operator triage.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX idx_tenants_backoffice_created
    ON tenants (created_at DESC, id DESC);

CREATE INDEX idx_tenants_backoffice_status_plan_created
    ON tenants (status, plan, created_at DESC, id DESC);

CREATE INDEX idx_tenants_backoffice_name_trgm
    ON tenants USING gin (name gin_trgm_ops);

CREATE INDEX idx_tenants_backoffice_slug_trgm
    ON tenants USING gin ((slug::text) gin_trgm_ops);

CREATE INDEX idx_tenant_members_backoffice_recovery_candidates
    ON tenant_members (tenant_id, created_at DESC, id DESC)
    WHERE status = 'active' AND last_login_at IS NOT NULL;
