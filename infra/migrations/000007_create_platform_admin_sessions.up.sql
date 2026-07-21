CREATE TABLE platform_admin_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id uuid NOT NULL REFERENCES platform_admins(id) ON DELETE CASCADE,
    refresh_hash text NOT NULL UNIQUE CHECK (char_length(refresh_hash) = 64),
    issued_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    user_agent text,
    ip inet,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_platform_admin_sessions_admin_id ON platform_admin_sessions (admin_id);

REVOKE ALL ON platform_admin_sessions FROM PUBLIC, fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON platform_admin_sessions TO fidibeki_admin;
