DROP INDEX idx_tenant_access_events_dedup;
DROP TRIGGER tenant_access_events_classify_actor ON tenant_access_events;
DROP FUNCTION tenant_access_events_classify_actor();

ALTER TABLE tenant_access_events
    DROP CONSTRAINT tenant_access_events_actor_consistency;

-- Migration 84 could not represent an actor after its live identity row was
-- deleted. Restore that legacy representation before reinstating its check.
UPDATE tenant_access_events
SET actor_type = 'system'
WHERE (actor_type = 'human' AND actor_member_id IS NULL)
   OR (actor_type = 'api_key' AND actor_api_key_id IS NULL)
   OR (actor_type = 'platform_admin' AND actor_platform_admin_id IS NULL);

ALTER TABLE tenant_access_events
    DROP COLUMN actor_id;

ALTER TABLE tenant_access_events
    ADD CONSTRAINT tenant_access_events_actor_consistency CHECK (
        (actor_type = 'human' AND actor_member_id IS NOT NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NULL)
        OR (actor_type = 'api_key' AND actor_member_id IS NULL
            AND actor_api_key_id IS NOT NULL AND actor_platform_admin_id IS NULL)
        OR (actor_type = 'platform_admin' AND actor_member_id IS NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NOT NULL)
        OR (actor_type = 'system' AND actor_member_id IS NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NULL)
    );

CREATE FUNCTION tenant_access_events_classify_actor() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.actor_type := CASE
        WHEN NEW.actor_api_key_id IS NOT NULL THEN 'api_key'
        WHEN NEW.actor_platform_admin_id IS NOT NULL THEN 'platform_admin'
        WHEN NEW.actor_member_id IS NOT NULL THEN 'human'
        ELSE 'system'
    END;
    RETURN NEW;
END;
$$;

CREATE TRIGGER tenant_access_events_classify_actor
BEFORE INSERT OR UPDATE OF actor_member_id, actor_api_key_id, actor_platform_admin_id, actor_type
ON tenant_access_events
FOR EACH ROW EXECUTE FUNCTION tenant_access_events_classify_actor();

CREATE UNIQUE INDEX idx_tenant_access_events_dedup ON tenant_access_events (
    tenant_id, resource_type, resource_id, actor_type,
    COALESCE(actor_member_id::text, actor_api_key_id::text, actor_platform_admin_id::text, ''),
    action, outcome, dedup_bucket
) WHERE resource_id IS NOT NULL AND dedup_bucket IS NOT NULL;
