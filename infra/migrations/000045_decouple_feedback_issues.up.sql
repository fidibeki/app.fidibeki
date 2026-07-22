ALTER TABLE feedbacks
    ADD COLUMN title text,
    ADD COLUMN description text,
    ADD COLUMN ai_suggested_title text,
    ADD COLUMN ai_actionability text,
    ADD COLUMN ai_category text,
    ADD COLUMN ai_priority text;

UPDATE feedbacks
SET title = CASE
        WHEN NULLIF(btrim(text), '') IS NULL THEN 'Feedback without written detail'
        ELSE left(btrim(COALESCE(
            substring(
                regexp_replace(btrim(text), '[[:space:]]+', ' ', 'g')
                from '^.*?[.!?](?:[[:space:]]|$)'
            ),
            regexp_replace(btrim(text), '[[:space:]]+', ' ', 'g')
        )), 80)
    END,
    description = text;

ALTER TABLE feedbacks
    ALTER COLUMN title SET DEFAULT 'Feedback without written detail',
    ALTER COLUMN title SET NOT NULL,
    ADD CONSTRAINT feedbacks_title_length CHECK (char_length(title) BETWEEN 1 AND 80),
    ADD CONSTRAINT feedbacks_description_length CHECK (description IS NULL OR char_length(description) <= 10000),
    ADD CONSTRAINT feedbacks_ai_suggested_title_length CHECK (
        ai_suggested_title IS NULL OR char_length(ai_suggested_title) BETWEEN 1 AND 80
    ),
    ADD CONSTRAINT feedbacks_ai_actionability_valid CHECK (
        ai_actionability IS NULL OR ai_actionability IN ('actionable', 'not_actionable', 'uncertain')
    ),
    ADD CONSTRAINT feedbacks_ai_category_length CHECK (
        ai_category IS NULL OR char_length(ai_category) BETWEEN 1 AND 80
    ),
    ADD CONSTRAINT feedbacks_ai_priority_valid CHECK (
        ai_priority IS NULL OR ai_priority IN ('low', 'medium', 'high', 'urgent')
    );

ALTER TABLE issues
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD CONSTRAINT issues_description_length CHECK (char_length(description) <= 10000);

UPDATE issues AS issue
SET description = COALESCE((
    SELECT feedback.description
    FROM feedbacks AS feedback
    WHERE feedback.issue_id = issue.id AND feedback.description IS NOT NULL
    ORDER BY feedback.created_at, feedback.id
    LIMIT 1
), '');
