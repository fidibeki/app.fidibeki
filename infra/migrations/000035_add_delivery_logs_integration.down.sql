DROP INDEX idx_delivery_logs_destination_attempt_unique;
DROP INDEX idx_delivery_logs_integration_id;

-- Phase 8 cannot represent more than one delivery attempt per outbox event.
DELETE FROM delivery_logs WHERE integration_id IS NOT NULL;

ALTER TABLE delivery_logs
    DROP CONSTRAINT delivery_logs_tenant_integration_fkey,
    DROP COLUMN integration_id,
    DROP COLUMN delivery_key,
    ADD CONSTRAINT delivery_logs_outbox_attempt_key UNIQUE (outbox_event_id, attempt_number);
