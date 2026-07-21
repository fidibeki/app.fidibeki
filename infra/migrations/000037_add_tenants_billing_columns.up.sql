ALTER TABLE tenants
    ADD COLUMN pending_plan tenant_plan,
    ADD COLUMN email_bounced boolean NOT NULL DEFAULT false,
    ADD COLUMN stripe_customer_id text,
    ADD COLUMN stripe_subscription_id text,
    ADD COLUMN deletion_requested_at timestamptz,
    ADD COLUMN deletion_requested_by_member_id uuid;

CREATE UNIQUE INDEX idx_tenants_stripe_customer_id
    ON tenants (stripe_customer_id)
    WHERE stripe_customer_id IS NOT NULL;

CREATE UNIQUE INDEX idx_tenants_stripe_subscription_id
    ON tenants (stripe_subscription_id)
    WHERE stripe_subscription_id IS NOT NULL;
