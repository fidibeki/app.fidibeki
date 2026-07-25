ALTER TABLE tenants
    DROP CONSTRAINT tenants_billing_interval_valid,
    DROP COLUMN billing_interval;
