DROP TABLE IF EXISTS reporter_clarification_requests;
DROP TABLE IF EXISTS reporter_access_tokens;

ALTER TABLE issues DROP COLUMN IF EXISTS reporter_access_revoked_at;
ALTER TABLE projects DROP COLUMN IF EXISTS reporter_access_enabled;
ALTER TABLE tenants DROP COLUMN IF EXISTS reporter_access_enabled;

-- PostgreSQL enum values cannot be removed safely in a reversible migration. The issue_status type
-- is dropped by migration 000017 during a full rollback, and the forward migration is idempotent.
