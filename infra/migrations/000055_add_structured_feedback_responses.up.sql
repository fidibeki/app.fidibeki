ALTER TABLE feedbacks
    ADD COLUMN widget_definition_version integer,
    ADD COLUMN responses jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN comment text,
    ADD CONSTRAINT feedbacks_widget_definition_version_valid CHECK (
        widget_definition_version IS NULL OR widget_definition_version >= 1
    ),
    ADD CONSTRAINT feedbacks_responses_object CHECK (jsonb_typeof(responses) = 'object'),
    ADD CONSTRAINT feedbacks_comment_length CHECK (comment IS NULL OR char_length(comment) <= 10000);

UPDATE feedbacks
SET responses =
    CASE WHEN rating IS NULL THEN '{}'::jsonb
         ELSE jsonb_build_object('rating', rating)
    END
    ||
    CASE WHEN text IS NULL THEN '{}'::jsonb
         ELSE jsonb_build_object('feedback', text)
    END;
