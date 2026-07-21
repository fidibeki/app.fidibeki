ALTER TABLE feedbacks
    DROP CONSTRAINT feedbacks_project_session_fkey,
    DROP CONSTRAINT feedbacks_tenant_session_fkey,
    DROP COLUMN session_id;
