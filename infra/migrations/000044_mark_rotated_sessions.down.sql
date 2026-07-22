SET lock_timeout = '5s';

ALTER TABLE tenant_member_sessions
    DROP COLUMN replacement_hash,
    DROP COLUMN rotated_at;

ALTER TABLE platform_admin_sessions
    DROP COLUMN replacement_hash,
    DROP COLUMN rotated_at;

RESET lock_timeout;
