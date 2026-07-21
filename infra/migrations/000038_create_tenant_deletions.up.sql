CREATE TABLE tenant_deletions (
    tenant_id uuid PRIMARY KEY,
    tenant_slug text NOT NULL,
    tenant_name text NOT NULL,
    requested_at timestamptz NOT NULL,
    executed_at timestamptz NOT NULL,
    requested_by_member_id uuid,
    forced_by_platform_admin_id uuid,
    feedback_count integer NOT NULL CHECK (feedback_count >= 0),
    attachment_count integer NOT NULL CHECK (attachment_count >= 0),
    CONSTRAINT tenant_deletions_actor_present CHECK (
        requested_by_member_id IS NOT NULL OR forced_by_platform_admin_id IS NOT NULL
    )
);

GRANT SELECT, INSERT ON tenant_deletions TO fidibeki_admin;
