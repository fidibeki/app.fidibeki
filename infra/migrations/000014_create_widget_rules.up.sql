CREATE TABLE widget_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    priority integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    matcher_type matcher_type NOT NULL,
    matcher_value text,
    trigger_type trigger_type NOT NULL,
    trigger_delay_seconds integer,
    trigger_scroll_percent integer,
    can_rate boolean NOT NULL DEFAULT true,
    can_write_text boolean NOT NULL DEFAULT true,
    can_record_audio boolean NOT NULL DEFAULT false,
    can_record_video boolean NOT NULL DEFAULT false,
    can_attach_element boolean NOT NULL DEFAULT false,
    can_attach_screenshot boolean NOT NULL DEFAULT false,
    greeting text CHECK (greeting IS NULL OR char_length(greeting) <= 500),
    position widget_position NOT NULL DEFAULT 'bottom-left',
    theme widget_theme NOT NULL DEFAULT 'light',
    accent_color text CHECK (accent_color IS NULL OR accent_color ~ '^#[0-9a-fA-F]{6}$'),
    replay_mode replay_mode NOT NULL DEFAULT 'none',
    replay_sampling_rate integer NOT NULL DEFAULT 0 CHECK (replay_sampling_rate BETWEEN 0 AND 100),
    replay_max_duration_seconds integer NOT NULL DEFAULT 1800 CHECK (replay_max_duration_seconds BETWEEN 1 AND 7200),
    replay_max_events integer NOT NULL DEFAULT 50000 CHECK (replay_max_events BETWEEN 1 AND 200000),
    replay_mask_all_inputs boolean NOT NULL DEFAULT true,
    replay_mask_text_selector text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT widget_rules_matcher_consistent CHECK (
        (matcher_type = 'all_pages' AND matcher_value IS NULL)
        OR (matcher_type <> 'all_pages' AND matcher_value IS NOT NULL AND matcher_value <> '')
    ),
    CONSTRAINT widget_rules_trigger_consistent CHECK (
        (trigger_type = 'delay' AND trigger_delay_seconds BETWEEN 0 AND 86400 AND trigger_scroll_percent IS NULL)
        OR (trigger_type = 'scroll' AND trigger_scroll_percent BETWEEN 1 AND 100 AND trigger_delay_seconds IS NULL)
        OR (trigger_type = 'exit_intent' AND trigger_delay_seconds IS NULL AND trigger_scroll_percent IS NULL)
    ),
    CONSTRAINT widget_rules_replay_consistent CHECK (
        (replay_mode = 'sampling' AND replay_sampling_rate BETWEEN 1 AND 100)
        OR (replay_mode <> 'sampling' AND replay_sampling_rate = 0)
    )
);

CREATE INDEX idx_widget_rules_project_id ON widget_rules (project_id);
CREATE INDEX idx_widget_rules_enabled ON widget_rules (enabled);
CREATE INDEX idx_widget_rules_priority ON widget_rules (project_id, priority DESC);

CREATE TRIGGER widget_rules_set_updated_at
BEFORE UPDATE ON widget_rules
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE widget_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE widget_rules FORCE ROW LEVEL SECURITY;

CREATE POLICY widget_rules_tenant_isolation ON widget_rules
    FOR ALL TO fidibeki_app
    USING (EXISTS (
        SELECT 1 FROM projects
        WHERE projects.id = widget_rules.project_id
          AND projects.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM projects
        WHERE projects.id = widget_rules.project_id
          AND projects.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    ));

GRANT SELECT, INSERT, UPDATE, DELETE ON widget_rules TO fidibeki_app, fidibeki_admin;
