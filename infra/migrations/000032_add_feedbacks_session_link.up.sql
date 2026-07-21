ALTER TABLE feedbacks ADD COLUMN session_id uuid;

ALTER TABLE feedbacks
    ADD CONSTRAINT feedbacks_tenant_session_fkey FOREIGN KEY (tenant_id, session_id)
        REFERENCES sessions(tenant_id, id) ON DELETE SET NULL (session_id),
    ADD CONSTRAINT feedbacks_project_session_fkey FOREIGN KEY (project_id, session_id)
        REFERENCES sessions(project_id, id) ON DELETE SET NULL (session_id);

CREATE INDEX idx_feedbacks_session_id ON feedbacks (session_id)
    WHERE session_id IS NOT NULL;
