ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_comment_length,
    DROP CONSTRAINT feedbacks_responses_object,
    DROP CONSTRAINT feedbacks_widget_definition_version_valid,
    DROP COLUMN comment,
    DROP COLUMN responses,
    DROP COLUMN widget_definition_version;
