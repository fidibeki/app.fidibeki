DELETE FROM outbox_events
WHERE event::text IN ('tenant.owner_activated', 'tenant.deletion_cancelled', 'billing.state_changed');

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN ('feedback', 'issue'));

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_data_object,
    DROP COLUMN data;

ALTER TABLE tenants DROP COLUMN welcome_email_sent_at;

-- PostgreSQL enum labels cannot be removed in place. A full rollback later drops event_type in
-- migration 18; reapplying this migration is safe because the additions use IF NOT EXISTS.
