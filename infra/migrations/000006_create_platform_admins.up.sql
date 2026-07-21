CREATE TABLE platform_admins (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email citext NOT NULL UNIQUE,
    name text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 120),
    permissions text[] NOT NULL DEFAULT '{}',
    email_bounced boolean NOT NULL DEFAULT false,
    email_complained boolean NOT NULL DEFAULT false,
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_admins_email_valid CHECK (email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);

CREATE TRIGGER platform_admins_set_updated_at
BEFORE UPDATE ON platform_admins
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

REVOKE ALL ON platform_admins FROM PUBLIC, fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON platform_admins TO fidibeki_admin;
