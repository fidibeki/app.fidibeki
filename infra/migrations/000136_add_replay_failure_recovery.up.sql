ALTER TABLE sessions
    ADD COLUMN failure_code text,
    ADD COLUMN merge_generation integer NOT NULL DEFAULT 1,
    ADD CONSTRAINT sessions_failure_code_check CHECK (
        failure_code IS NULL OR failure_code IN (
            'contract_violation',
            'unsafe_payload',
            'upload_expired',
            'processing_failed',
            'unavailable'
        )
    ),
    ADD CONSTRAINT sessions_merge_generation_check CHECK (merge_generation > 0);

UPDATE sessions AS session
SET failure_code = CASE
    WHEN EXISTS (
        SELECT 1
        FROM session_batches AS batch
        WHERE batch.session_id = session.id
          AND batch.safety_status = 'rejected'
    ) THEN 'unsafe_payload'
    WHEN EXISTS (
        SELECT 1
        FROM session_batches AS batch
        WHERE batch.session_id = session.id
          AND batch.safety_status = 'expired'
    ) THEN 'upload_expired'
    WHEN session.merge_requested_at IS NOT NULL
      AND session.merge_error IS NOT NULL
      AND session.merge_attempts >= 12
      AND EXISTS (
          SELECT 1
          FROM session_batches AS batch
          WHERE batch.session_id = session.id
            AND batch.safety_status = 'safe'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM session_batches AS batch
          WHERE batch.session_id = session.id
            AND batch.safety_status <> 'safe'
      ) THEN 'processing_failed'
    WHEN session.merge_error IS NOT NULL
      AND session.merge_attempts >= 12 THEN 'unavailable'
    ELSE NULL
END
WHERE session.merged_at IS NULL
  AND session.retention_deleted_at IS NULL
  AND (
      session.merge_error IS NOT NULL
      OR EXISTS (
          SELECT 1
          FROM session_batches AS batch
          WHERE batch.session_id = session.id
            AND batch.safety_status IN ('rejected', 'expired')
      )
  );
