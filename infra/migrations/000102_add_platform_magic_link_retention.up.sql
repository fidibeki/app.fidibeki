-- Platform admin magic-link retention tracer (issue #62): the platform-admin
-- equivalent of tenant_magic_links' expiry sweep (migration 98).
-- ConsumeMagicLink already rejects platform_admin_magic_links rows once
-- expires_at <= now(), so a swept row never had remaining authentication
-- function; deleting it removes stored credential material rather than
-- inventing a new retention period. Platform-admin actions have no tenant
-- scope, so this also adds the minimal global, append-only security-event
-- ledger needed to evidence that deletion without resolving platform-admin
-- session, IP/user-agent, or general security-audit retention.

CREATE INDEX idx_platform_admin_magic_links_expires_at
    ON platform_admin_magic_links (expires_at, id);

CREATE TABLE platform_security_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    resource_type text NOT NULL CHECK (char_length(resource_type) BETWEEN 1 AND 100),
    resource_id uuid NOT NULL,
    data jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_security_events_identity_key
        UNIQUE (kind, resource_type, resource_id)
);

REVOKE ALL ON platform_security_events FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON platform_security_events TO fidibeki_admin;
