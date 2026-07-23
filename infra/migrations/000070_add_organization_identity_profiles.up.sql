ALTER TABLE tenants
    ADD COLUMN short_name text,
    ADD COLUMN website_url text,
    ADD COLUMN support_email citext,
    ADD COLUMN default_locale text,
    ADD COLUMN default_timezone text,
    ADD COLUMN privacy_policy_url text,
    ADD COLUMN terms_url text,
    ADD COLUMN logo_version uuid,
    ADD CONSTRAINT tenants_short_name_valid CHECK (
        short_name IS NULL OR char_length(short_name) BETWEEN 1 AND 32
    ),
    ADD CONSTRAINT tenants_website_url_length CHECK (
        website_url IS NULL OR char_length(website_url) <= 2048
    ),
    ADD CONSTRAINT tenants_support_email_valid CHECK (
        support_email IS NULL OR (
            char_length(support_email::text) <= 320
            AND support_email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
        )
    ),
    ADD CONSTRAINT tenants_default_locale_valid CHECK (
        default_locale IS NULL OR (
            char_length(default_locale) <= 35
            AND default_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
        )
    ),
    ADD CONSTRAINT tenants_default_timezone_valid CHECK (
        default_timezone IS NULL OR (
            char_length(default_timezone) <= 100
            AND (
                default_timezone = 'UTC'
                OR default_timezone ~ '^[A-Za-z][A-Za-z0-9_+-]*/[A-Za-z0-9_+./-]+$'
            )
        )
    ),
    ADD CONSTRAINT tenants_privacy_policy_url_length CHECK (
        privacy_policy_url IS NULL OR char_length(privacy_policy_url) <= 2048
    ),
    ADD CONSTRAINT tenants_terms_url_length CHECK (
        terms_url IS NULL OR char_length(terms_url) <= 2048
    );

ALTER TABLE tenant_members
    ADD COLUMN avatar_version uuid;

CREATE TABLE identity_asset_deletions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    asset_kind text NOT NULL CHECK (asset_kind IN ('workspace_logo', 'member_avatar')),
    member_id uuid,
    version uuid NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text CHECK (last_error IS NULL OR char_length(last_error) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identity_asset_deletions_subject_valid CHECK (
        (asset_kind = 'workspace_logo' AND member_id IS NULL)
        OR (asset_kind = 'member_avatar' AND member_id IS NOT NULL)
    ),
    CONSTRAINT identity_asset_deletions_unique
        UNIQUE NULLS NOT DISTINCT (tenant_id, asset_kind, member_id, version)
);

CREATE INDEX idx_identity_asset_deletions_due
    ON identity_asset_deletions (next_attempt_at, created_at, id);

ALTER TABLE identity_asset_deletions ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity_asset_deletions FORCE ROW LEVEL SECURITY;

CREATE POLICY identity_asset_deletions_tenant_insert ON identity_asset_deletions
    FOR INSERT TO fidibeki_app
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT UPDATE (
    name, short_name, website_url, support_email, default_locale, default_timezone,
    privacy_policy_url, terms_url, logo_version
) ON tenants TO fidibeki_app;
GRANT INSERT ON identity_asset_deletions TO fidibeki_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON identity_asset_deletions TO fidibeki_admin;
