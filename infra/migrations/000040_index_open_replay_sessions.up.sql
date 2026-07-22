CREATE INDEX idx_sessions_open_started_at
    ON sessions (started_at, id)
    WHERE ended_at IS NULL;
