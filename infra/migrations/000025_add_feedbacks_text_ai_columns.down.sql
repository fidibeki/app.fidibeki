DROP INDEX IF EXISTS idx_feedbacks_text_process_status;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_text_sentiment_range,
    DROP CONSTRAINT IF EXISTS feedbacks_text_processing_generation_nonnegative,
    DROP COLUMN IF EXISTS ai_processed_at,
    DROP COLUMN IF EXISTS text_processing_generation,
    DROP COLUMN IF EXISTS text_process_status,
    DROP COLUMN IF EXISTS text_moderation_flags,
    DROP COLUMN IF EXISTS text_sentiment_score,
    DROP COLUMN IF EXISTS text_cleaned;
