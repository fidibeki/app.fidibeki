DROP TABLE issue_tags;
DROP TABLE feedback_tags;
DROP TABLE tags;

DROP INDEX idx_issues_assignee_id;
ALTER TABLE issues
    DROP CONSTRAINT issues_tenant_assignee_fkey,
    DROP CONSTRAINT issues_priority_valid,
    DROP COLUMN assignee_id,
    DROP COLUMN priority;

DROP INDEX idx_feedbacks_assignee_id;
ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_tenant_assignee_fkey,
    DROP CONSTRAINT feedbacks_priority_valid,
    DROP COLUMN assignee_id,
    DROP COLUMN priority;
