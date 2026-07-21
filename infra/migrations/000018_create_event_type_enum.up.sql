CREATE TYPE event_type AS ENUM ('feedback.created', 'issue.created', 'issue.updated', 'issue.resolved');

GRANT USAGE ON TYPE event_type TO fidibeki_app, fidibeki_admin;
