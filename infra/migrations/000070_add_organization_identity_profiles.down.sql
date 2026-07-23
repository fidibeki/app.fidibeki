DROP TABLE identity_asset_deletions;

ALTER TABLE tenant_members
    DROP COLUMN avatar_version;

ALTER TABLE tenants
    DROP COLUMN logo_version,
    DROP COLUMN terms_url,
    DROP COLUMN privacy_policy_url,
    DROP COLUMN default_timezone,
    DROP COLUMN default_locale,
    DROP COLUMN support_email,
    DROP COLUMN website_url,
    DROP COLUMN short_name;
