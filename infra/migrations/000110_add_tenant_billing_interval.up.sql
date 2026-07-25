ALTER TABLE tenants
    ADD COLUMN billing_interval text,
    ADD CONSTRAINT tenants_billing_interval_valid CHECK (
        billing_interval IN ('monthly', 'yearly')
    );

-- Every subscription that existed before this migration was created from a
-- monthly price; yearly prices are introduced together with this column.
UPDATE tenants
SET billing_interval = 'monthly'
WHERE stripe_subscription_id IS NOT NULL;
