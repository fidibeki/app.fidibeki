DROP INDEX IF EXISTS idx_attachments_ai_budget_deferred;
DROP INDEX IF EXISTS idx_feedbacks_ai_budget_deferred;

ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_ai_budget_deferred_consistent,
    DROP COLUMN IF EXISTS ai_budget_deferred_until;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_ai_budget_deferred_consistent,
    DROP CONSTRAINT IF EXISTS feedbacks_ai_budget_deferred_reason_length,
    DROP COLUMN IF EXISTS ai_budget_deferred_reason,
    DROP COLUMN IF EXISTS ai_budget_deferred_until;

DROP TABLE IF EXISTS ai_work_reservations;
DROP TABLE IF EXISTS ai_project_daily_usage;
