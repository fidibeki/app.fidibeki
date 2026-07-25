SET lock_timeout = '5s';

ALTER TABLE tenant_member_sessions
    DROP COLUMN two_factor_verified_at,
    DROP COLUMN two_factor_state,
    DROP COLUMN authentication_method;

REVOKE ALL ON tenant_member_recovery_codes FROM fidibeki_app, fidibeki_admin;
DROP TABLE tenant_member_recovery_codes;

REVOKE ALL ON tenant_member_totp FROM fidibeki_app, fidibeki_admin;
DROP TABLE tenant_member_totp;

ALTER TABLE tenants
    DROP COLUMN two_factor_policy_updated_by,
    DROP COLUMN two_factor_policy_updated_at,
    DROP COLUMN two_factor_policy;

RESET lock_timeout;
