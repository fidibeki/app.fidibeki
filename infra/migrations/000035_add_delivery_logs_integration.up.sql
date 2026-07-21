ALTER TABLE delivery_logs
    DROP CONSTRAINT delivery_logs_outbox_attempt_key,
    ADD COLUMN integration_id uuid,
    ADD COLUMN delivery_key text NOT NULL DEFAULT 'reporter'
        CHECK (char_length(delivery_key) BETWEEN 1 AND 100),
    ADD CONSTRAINT delivery_logs_tenant_integration_fkey FOREIGN KEY (tenant_id, integration_id)
        REFERENCES integrations(tenant_id, id) ON DELETE SET NULL (integration_id);

CREATE INDEX idx_delivery_logs_integration_id ON delivery_logs (integration_id)
    WHERE integration_id IS NOT NULL;
CREATE UNIQUE INDEX idx_delivery_logs_destination_attempt_unique
    ON delivery_logs (outbox_event_id, delivery_key, attempt_number);
