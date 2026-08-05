-- The tenant dashboard lists every replay session newest-first with keyset
-- pagination on (started_at, id). The existing single-column tenant index
-- forces a sort per page; this composite index serves the scan directly.
CREATE INDEX idx_sessions_tenant_list
    ON sessions (tenant_id, started_at DESC, id DESC);
