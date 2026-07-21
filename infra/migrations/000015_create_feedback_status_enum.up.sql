CREATE TYPE feedback_status AS ENUM ('open', 'spam', 'archived');

GRANT USAGE ON TYPE feedback_status TO fidibeki_app, fidibeki_admin;
