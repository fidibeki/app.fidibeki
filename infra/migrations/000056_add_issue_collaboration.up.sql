CREATE TABLE issue_activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    actor_member_id uuid,
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    visibility text NOT NULL DEFAULT 'internal' CHECK (visibility IN ('internal', 'reporter')),
    data jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_activities_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT issue_activities_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_activities_tenant_actor_fkey FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id)
);

CREATE INDEX idx_issue_activities_issue_created
    ON issue_activities (tenant_id, issue_id, created_at, id);

CREATE TABLE issue_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    author_member_id uuid,
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 5000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_comments_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_comments_tenant_author_fkey FOREIGN KEY (tenant_id, author_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (author_member_id),
    CONSTRAINT issue_comments_tenant_id_id_key UNIQUE (tenant_id, id)
);

CREATE INDEX idx_issue_comments_issue_created
    ON issue_comments (tenant_id, issue_id, created_at, id);

CREATE TABLE issue_comment_mentions (
    tenant_id uuid NOT NULL,
    comment_id uuid NOT NULL,
    member_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, member_id),
    CONSTRAINT issue_comment_mentions_tenant_comment_fkey FOREIGN KEY (tenant_id, comment_id)
        REFERENCES issue_comments(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_comment_mentions_tenant_member_fkey FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_issue_comment_mentions_member
    ON issue_comment_mentions (tenant_id, member_id, created_at DESC);

CREATE TABLE member_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    recipient_member_id uuid NOT NULL,
    actor_member_id uuid,
    issue_id uuid NOT NULL,
    activity_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT member_notifications_tenant_recipient_fkey FOREIGN KEY (tenant_id, recipient_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT member_notifications_tenant_actor_fkey FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id),
    CONSTRAINT member_notifications_tenant_issue_fkey FOREIGN KEY (tenant_id, issue_id)
        REFERENCES issues(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT member_notifications_tenant_activity_fkey FOREIGN KEY (tenant_id, activity_id)
        REFERENCES issue_activities(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT member_notifications_activity_recipient_key UNIQUE (activity_id, recipient_member_id)
);

CREATE INDEX idx_member_notifications_recipient_created
    ON member_notifications (tenant_id, recipient_member_id, created_at DESC, id DESC);
CREATE INDEX idx_member_notifications_unread
    ON member_notifications (tenant_id, recipient_member_id, created_at DESC)
    WHERE read_at IS NULL;

INSERT INTO issue_activities (
    tenant_id, issue_id, actor_member_id, kind, visibility, data, created_at
)
SELECT issue_update.tenant_id, issue_update.issue_id, issue_update.member_id,
       'issue.status_changed', 'internal',
       jsonb_strip_nulls(jsonb_build_object(
           'changes', jsonb_build_object(
               'status', jsonb_build_object(
                   'from', issue_update.old_status,
                   'to', issue_update.new_status
               )
           ),
           'transition_comment', issue_update.comment,
           'legacy_update_id', issue_update.id
       )),
       issue_update.created_at
FROM issue_updates AS issue_update;

ALTER TABLE issue_activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_activities FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_activities_tenant_select ON issue_activities
    FOR SELECT TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY issue_activities_tenant_insert ON issue_activities
    FOR INSERT TO fidibeki_app
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE issue_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_comments_tenant_select ON issue_comments
    FOR SELECT TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY issue_comments_tenant_insert ON issue_comments
    FOR INSERT TO fidibeki_app
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE issue_comment_mentions ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_comment_mentions FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_comment_mentions_tenant_select ON issue_comment_mentions
    FOR SELECT TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY issue_comment_mentions_tenant_insert ON issue_comment_mentions
    FOR INSERT TO fidibeki_app
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE member_notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY member_notifications_tenant_isolation ON member_notifications
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON issue_activities, issue_comments, issue_comment_mentions
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON member_notifications TO fidibeki_app, fidibeki_admin;
