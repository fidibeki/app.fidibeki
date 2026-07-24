-- Identity rows have a shorter lifecycle than the append-only access ledger.
-- Preserve the actor UUID separately from the nullable live foreign key so an
-- identity deletion cannot erase who performed a sensitive-data access.
ALTER TABLE tenant_access_events
    ADD COLUMN actor_id uuid;

UPDATE tenant_access_events
SET actor_id = COALESCE(actor_member_id, actor_api_key_id, actor_platform_admin_id);

DROP INDEX idx_tenant_access_events_dedup;
DROP TRIGGER tenant_access_events_classify_actor ON tenant_access_events;
DROP FUNCTION tenant_access_events_classify_actor();

ALTER TABLE tenant_access_events
    DROP CONSTRAINT tenant_access_events_actor_consistency;

ALTER TABLE tenant_access_events
    ADD CONSTRAINT tenant_access_events_actor_consistency CHECK (
        (actor_type = 'human' AND actor_id IS NOT NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NULL
            AND (actor_member_id IS NULL OR actor_member_id = actor_id))
        OR (actor_type = 'api_key' AND actor_id IS NOT NULL
            AND actor_member_id IS NULL AND actor_platform_admin_id IS NULL
            AND (actor_api_key_id IS NULL OR actor_api_key_id = actor_id))
        OR (actor_type = 'platform_admin' AND actor_id IS NOT NULL
            AND actor_member_id IS NULL AND actor_api_key_id IS NULL
            AND (actor_platform_admin_id IS NULL OR actor_platform_admin_id = actor_id))
        OR (actor_type = 'system' AND actor_id IS NULL
            AND actor_member_id IS NULL AND actor_api_key_id IS NULL
            AND actor_platform_admin_id IS NULL)
    );

-- Older writers only populate a live identity column. Classify and copy its ID
-- once at insert time. The trigger deliberately does not run when a foreign key
-- is cleared by ON DELETE SET NULL.
CREATE FUNCTION tenant_access_events_classify_actor() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.actor_type := CASE
        WHEN NEW.actor_api_key_id IS NOT NULL THEN 'api_key'
        WHEN NEW.actor_platform_admin_id IS NOT NULL THEN 'platform_admin'
        WHEN NEW.actor_member_id IS NOT NULL THEN 'human'
        ELSE 'system'
    END;
    NEW.actor_id := COALESCE(
        NEW.actor_member_id,
        NEW.actor_api_key_id,
        NEW.actor_platform_admin_id
    );
    RETURN NEW;
END;
$$;

CREATE TRIGGER tenant_access_events_classify_actor
BEFORE INSERT ON tenant_access_events
FOR EACH ROW EXECUTE FUNCTION tenant_access_events_classify_actor();

CREATE UNIQUE INDEX idx_tenant_access_events_dedup ON tenant_access_events (
    tenant_id, resource_type, resource_id, actor_type,
    COALESCE(actor_id::text, ''),
    action, outcome, dedup_bucket
) WHERE resource_id IS NOT NULL AND dedup_bucket IS NOT NULL;
