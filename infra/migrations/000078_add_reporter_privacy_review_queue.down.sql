DROP TABLE IF EXISTS reporter_privacy_review_items;
DROP FUNCTION IF EXISTS reporter_privacy_review_items_lock_decided();
DROP TYPE IF EXISTS reporter_privacy_review_status;
DROP TABLE IF EXISTS reporter_privacy_request_scope;

-- PostgreSQL enum values cannot be removed safely in a reversible migration. Both types are
-- dropped entirely during a full rollback of their creating migrations, and the forward
-- migration is idempotent (IF NOT EXISTS).
