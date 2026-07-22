ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_description_length,
    DROP COLUMN IF EXISTS description;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_ai_priority_valid,
    DROP CONSTRAINT IF EXISTS feedbacks_ai_category_length,
    DROP CONSTRAINT IF EXISTS feedbacks_ai_actionability_valid,
    DROP CONSTRAINT IF EXISTS feedbacks_ai_suggested_title_length,
    DROP CONSTRAINT IF EXISTS feedbacks_description_length,
    DROP CONSTRAINT IF EXISTS feedbacks_title_length,
    DROP COLUMN IF EXISTS ai_priority,
    DROP COLUMN IF EXISTS ai_category,
    DROP COLUMN IF EXISTS ai_actionability,
    DROP COLUMN IF EXISTS ai_suggested_title,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS title;
