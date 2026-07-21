ALTER TABLE feedbacks
    ADD COLUMN text_cleaned text,
    ADD COLUMN text_sentiment_score numeric(3,2),
    ADD COLUMN text_moderation_flags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN text_process_status processing_status NOT NULL DEFAULT 'pending',
    ADD COLUMN text_processing_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN ai_processed_at timestamptz,
    ADD CONSTRAINT feedbacks_text_sentiment_range CHECK (
        text_sentiment_score IS NULL OR text_sentiment_score BETWEEN -1.0 AND 1.0
    ),
    ADD CONSTRAINT feedbacks_text_processing_generation_nonnegative CHECK (
        text_processing_generation >= 0
    );

UPDATE feedbacks AS feedback
SET text_process_status = CASE
    WHEN NULLIF(btrim(feedback.text), '') IS NULL OR tenant.plan = 'starter' THEN 'skipped'::processing_status
    ELSE 'pending'::processing_status
END
FROM tenants AS tenant
WHERE tenant.id = feedback.tenant_id;

CREATE INDEX idx_feedbacks_text_process_status
    ON feedbacks (tenant_id, text_process_status);
