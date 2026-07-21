CREATE TYPE processing_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'skipped');

GRANT USAGE ON TYPE processing_status TO fidibeki_app, fidibeki_admin;
