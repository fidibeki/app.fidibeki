ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM fidibeki_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE USAGE, SELECT ON SEQUENCES FROM fidibeki_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE EXECUTE ON FUNCTIONS FROM fidibeki_admin;

REVOKE USAGE ON SCHEMA public FROM fidibeki_app, fidibeki_admin;
REVOKE EXECUTE ON FUNCTION set_updated_at() FROM fidibeki_app, fidibeki_admin;

-- Cluster roles intentionally outlive a database migration rollback. Production credentials
-- are provisioned by the platform, while local login attributes are managed by the init script.
