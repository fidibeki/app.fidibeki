CREATE TYPE delivery_status AS ENUM ('pending', 'sent', 'failed', 'bounced');

GRANT USAGE ON TYPE delivery_status TO fidibeki_app, fidibeki_admin;
