ALTER TABLE attachments
    ADD COLUMN error_message text,
    ADD COLUMN processing_attempts integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT attachments_processing_attempts_nonnegative CHECK (processing_attempts >= 0),
    ADD CONSTRAINT attachments_error_message_length CHECK (
        error_message IS NULL OR char_length(error_message) <= 2000
    );

UPDATE attachments AS attachment
SET processing_status = 'skipped', error_message = NULL
FROM feedbacks AS feedback
JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
WHERE attachment.feedback_id = feedback.id
  AND (attachment.kind = 'replay' OR tenant.plan = 'starter')
  AND attachment.processing_status IN ('pending', 'processing');

UPDATE feedbacks AS feedback
SET ai_processed_at = COALESCE(feedback.ai_processed_at, now())
WHERE feedback.text_process_status IN ('completed', 'failed', 'skipped')
  AND NOT EXISTS (
    SELECT 1
    FROM attachments AS attachment
    WHERE attachment.feedback_id = feedback.id
      AND attachment.processing_status NOT IN ('completed', 'failed', 'skipped')
  );
