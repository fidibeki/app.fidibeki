DROP INDEX idx_tenants_stripe_subscription_id;
DROP INDEX idx_tenants_stripe_customer_id;

ALTER TABLE tenants
    DROP COLUMN deletion_requested_by_member_id,
    DROP COLUMN deletion_requested_at,
    DROP COLUMN stripe_subscription_id,
    DROP COLUMN stripe_customer_id,
    DROP COLUMN email_bounced,
    DROP COLUMN pending_plan;
