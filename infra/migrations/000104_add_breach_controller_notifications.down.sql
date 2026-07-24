DROP TABLE IF EXISTS breach_controller_notification_outbox;
DROP TABLE IF EXISTS breach_controller_notification_attempts;
DROP TABLE IF EXISTS breach_controller_notifications;
DROP FUNCTION IF EXISTS breach_controller_notification_content_guard();
DROP TYPE IF EXISTS breach_controller_notification_attempt_status;
DROP TYPE IF EXISTS breach_controller_notification_status;
DROP TYPE IF EXISTS breach_controller_notification_kind;
