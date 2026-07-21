CREATE TABLE issue_counters (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    year integer NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    next_seq integer NOT NULL DEFAULT 1 CHECK (next_seq > 0),
    PRIMARY KEY (tenant_id, year)
);

ALTER TABLE issue_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_counters FORCE ROW LEVEL SECURITY;

CREATE POLICY issue_counters_tenant_isolation ON issue_counters
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON issue_counters TO fidibeki_app, fidibeki_admin;
