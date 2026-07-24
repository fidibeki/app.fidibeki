DELETE FROM outbox_events WHERE event::text = 'feedback.rectified';

DROP TABLE IF EXISTS privacy_case_feedback_rectifications;

-- PostgreSQL enum labels cannot be removed in place. A full rollback later drops event_type in
-- migration 18; reapplying this migration is safe because the addition uses IF NOT EXISTS.
