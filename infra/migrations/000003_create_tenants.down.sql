REVOKE ALL ON tenants FROM fidibeki_app, fidibeki_admin;
REVOKE USAGE ON TYPE tenant_plan, tenant_status FROM fidibeki_app, fidibeki_admin;

DROP TABLE tenants;
DROP TYPE tenant_status;
DROP TYPE tenant_plan;
