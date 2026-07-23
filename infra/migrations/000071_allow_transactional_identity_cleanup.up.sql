CREATE POLICY identity_asset_deletions_tenant_select ON identity_asset_deletions
    FOR SELECT TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY identity_asset_deletions_tenant_update ON identity_asset_deletions
    FOR UPDATE TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT ON identity_asset_deletions TO fidibeki_app;
GRANT UPDATE (next_attempt_at) ON identity_asset_deletions TO fidibeki_app;
