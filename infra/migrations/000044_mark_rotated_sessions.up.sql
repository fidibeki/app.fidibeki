SET lock_timeout = '5s';

ALTER TABLE platform_admin_sessions
    ADD COLUMN rotated_at timestamptz,
    ADD COLUMN replacement_hash text CHECK (
        replacement_hash IS NULL OR char_length(replacement_hash) = 64
    );

ALTER TABLE tenant_member_sessions
    ADD COLUMN rotated_at timestamptz,
    ADD COLUMN replacement_hash text CHECK (
        replacement_hash IS NULL OR char_length(replacement_hash) = 64
    );

RESET lock_timeout;
