CREATE TABLE platform_admin_magic_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id uuid NOT NULL REFERENCES platform_admins(id) ON DELETE CASCADE,
    email citext NOT NULL,
    token_hash text NOT NULL UNIQUE CHECK (char_length(token_hash) = 64),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_admin_magic_links_expiry_valid CHECK (expires_at > created_at)
);

CREATE INDEX idx_platform_admin_magic_links_email ON platform_admin_magic_links (email);

REVOKE ALL ON platform_admin_magic_links FROM PUBLIC, fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON platform_admin_magic_links TO fidibeki_admin;
