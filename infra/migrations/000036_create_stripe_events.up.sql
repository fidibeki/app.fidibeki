CREATE TABLE stripe_events (
    event_id text PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT ON stripe_events TO fidibeki_admin;
