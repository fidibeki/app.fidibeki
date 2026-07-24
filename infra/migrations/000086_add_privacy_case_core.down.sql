DROP FUNCTION IF EXISTS fidibeki_privacy_session_restricted(uuid, uuid, text);
DROP FUNCTION IF EXISTS fidibeki_privacy_issue_restricted(uuid, uuid, text);
DROP FUNCTION IF EXISTS fidibeki_privacy_feedback_restricted(uuid, uuid, text);
DROP FUNCTION IF EXISTS fidibeki_privacy_email_restricted(uuid, text, text);

DROP TABLE IF EXISTS privacy_processing_restrictions;
DROP TRIGGER IF EXISTS privacy_case_events_immutable ON privacy_case_events;
DROP FUNCTION IF EXISTS privacy_case_events_append_only();
DROP TABLE IF EXISTS privacy_case_events;
DROP TABLE IF EXISTS privacy_cases;

DROP TYPE IF EXISTS privacy_case_status;
DROP TYPE IF EXISTS privacy_controller_role;
DROP TYPE IF EXISTS privacy_right_type;
DROP TYPE IF EXISTS privacy_subject_type;
