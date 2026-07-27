DROP INDEX idx_tenants_per_seat_subscriptions;

ALTER TABLE tenants
    DROP CONSTRAINT tenants_billing_seat_sync_error_valid,
    DROP CONSTRAINT tenants_billing_seat_quantity_valid,
    DROP CONSTRAINT tenants_billing_model_valid,
    DROP COLUMN billing_seat_sync_error,
    DROP COLUMN billing_seat_synced_at,
    DROP COLUMN billing_seat_sync_attempted_at,
    DROP COLUMN billing_seat_quantity,
    DROP COLUMN billing_model;
