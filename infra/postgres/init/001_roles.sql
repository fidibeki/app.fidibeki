DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fidibeki_app') THEN
        CREATE ROLE fidibeki_app LOGIN PASSWORD 'fidibeki-app-local';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fidibeki_admin') THEN
        CREATE ROLE fidibeki_admin LOGIN PASSWORD 'fidibeki-admin-local';
    END IF;
END;
$$;

ALTER ROLE fidibeki_app LOGIN PASSWORD 'fidibeki-app-local' NOBYPASSRLS;
ALTER ROLE fidibeki_admin LOGIN PASSWORD 'fidibeki-admin-local' BYPASSRLS;
GRANT CONNECT ON DATABASE fidibeki TO fidibeki_app, fidibeki_admin;
