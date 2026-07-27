ALTER TABLE tenants
    ADD COLUMN billing_model text NOT NULL DEFAULT 'per_seat',
    ADD COLUMN billing_seat_quantity integer NOT NULL DEFAULT 1,
    ADD COLUMN billing_seat_sync_attempted_at timestamptz,
    ADD COLUMN billing_seat_synced_at timestamptz,
    ADD COLUMN billing_seat_sync_error text,
    ADD CONSTRAINT tenants_billing_model_valid CHECK (
        billing_model IN ('legacy_flat', 'per_seat')
    ),
    ADD CONSTRAINT tenants_billing_seat_quantity_valid CHECK (
        billing_seat_quantity >= 1
    ),
    ADD CONSTRAINT tenants_billing_seat_sync_error_valid CHECK (
        billing_seat_sync_error IS NULL
        OR char_length(billing_seat_sync_error) BETWEEN 1 AND 500
    );

-- A deployment must not turn an existing flat subscription into a larger
-- invoice. Those tenants stay flat until a separately approved conversion.
UPDATE tenants
SET billing_model = 'legacy_flat',
    billing_seat_quantity = 1
WHERE stripe_subscription_id IS NOT NULL;

-- Trials and workspaces without a subscription are safe to initialize from
-- their durable membership state. Checkout will recount before calling Stripe.
UPDATE tenants AS tenant
SET billing_seat_quantity = GREATEST(
    1,
    (
        SELECT count(*)::integer
        FROM tenant_members AS member
        WHERE member.tenant_id = tenant.id
          AND member.status <> 'removed'
    )
)
WHERE tenant.stripe_subscription_id IS NULL;

CREATE INDEX idx_tenants_per_seat_subscriptions
    ON tenants (id)
    WHERE billing_model = 'per_seat'
      AND stripe_subscription_id IS NOT NULL;

