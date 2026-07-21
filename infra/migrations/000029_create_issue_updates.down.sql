DROP TABLE issue_updates;

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_key;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_key UNIQUE (event, resource_type, resource_id);
ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_tenant_id_id_key,
    DROP COLUMN event_key;
