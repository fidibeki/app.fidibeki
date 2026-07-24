DROP TABLE IF EXISTS breach_assessments;
DROP TYPE IF EXISTS breach_communication_decision;
DROP TYPE IF EXISTS breach_risk_decision;
DROP TYPE IF EXISTS breach_authority_notification_decision;
ALTER TABLE breach_incidents DROP COLUMN IF EXISTS authority_notification_deadline_at;
