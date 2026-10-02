ALTER TABLE website_chat_messages ADD CONSTRAINT website_chat_messages_source_key
    UNIQUE (tenant_id, project_id, visitor_id, id);

-- References only. Conversation contents remain under the chat retention policy.
CREATE TABLE website_chat_issue_links (
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    visitor_id uuid NOT NULL,
    message_id uuid PRIMARY KEY,
    issue_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, project_id, visitor_id, message_id)
        REFERENCES website_chat_messages(tenant_id, project_id, visitor_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, issue_id) REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE
);
CREATE INDEX website_chat_issue_links_conversation ON website_chat_issue_links(tenant_id, project_id, visitor_id);
CREATE INDEX website_chat_issue_links_issue ON website_chat_issue_links(tenant_id, project_id, issue_id);
ALTER TABLE website_chat_issue_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE website_chat_issue_links FORCE ROW LEVEL SECURITY;
CREATE POLICY website_chat_issue_links_scope ON website_chat_issue_links FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND project_id = NULLIF(current_setting('app.chat_project_id', true), '')::uuid
        AND current_setting('app.chat_operator', true) = 'true')
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND project_id = NULLIF(current_setting('app.chat_project_id', true), '')::uuid
        AND current_setting('app.chat_operator', true) = 'true');
GRANT SELECT, INSERT, DELETE ON website_chat_issue_links TO fidibeki_app, fidibeki_admin;
