ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'tenant.owner_activated';
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'tenant.deletion_cancelled';
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'billing.state_changed';

ALTER TABLE tenants ADD COLUMN welcome_email_sent_at timestamptz;

-- Existing workspaces must not receive a delayed founder welcome after this feature ships.
UPDATE tenants SET welcome_email_sent_at = created_at;

ALTER TABLE outbox_events
    ADD COLUMN data jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT outbox_events_data_object CHECK (jsonb_typeof(data) = 'object');

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN ('feedback', 'issue', 'tenant'));
