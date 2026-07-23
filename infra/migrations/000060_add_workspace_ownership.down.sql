REVOKE ALL ON tenant_access_events FROM fidibeki_app, fidibeki_admin;
DROP TABLE tenant_access_events;

REVOKE UPDATE (owner_member_id) ON tenants FROM fidibeki_app;
ALTER TABLE tenants DROP CONSTRAINT tenants_owner_member_fkey;
ALTER TABLE tenants DROP COLUMN owner_member_id;

ALTER TABLE tenant_members
    DROP COLUMN authorization_version,
    DROP COLUMN status;

REVOKE USAGE ON TYPE member_status FROM fidibeki_app, fidibeki_admin;
DROP TYPE member_status;
