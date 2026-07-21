CREATE TYPE issue_status AS ENUM ('open', 'in_progress', 'resolved', 'closed');

GRANT USAGE ON TYPE issue_status TO fidibeki_app, fidibeki_admin;
