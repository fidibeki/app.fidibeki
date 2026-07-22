CREATE TYPE widget_definition_status AS ENUM ('draft', 'active', 'paused', 'archived');
CREATE TYPE widget_presentation AS ENUM ('launcher', 'inline', 'reaction', 'prompt');
CREATE TYPE widget_objective AS ENUM (
    'continuous_feedback',
    'after_engagement',
    'validate_content',
    'detect_friction'
);

ALTER TABLE projects
    ADD COLUMN default_locale text NOT NULL DEFAULT 'en',
    ADD CONSTRAINT projects_default_locale_valid CHECK (
        default_locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    );

ALTER TABLE widget_rules
    ADD COLUMN definition_key text,
    ADD COLUMN definition_version integer NOT NULL DEFAULT 1,
    ADD COLUMN definition_status widget_definition_status,
    ADD COLUMN presentation widget_presentation,
    ADD COLUMN objective widget_objective NOT NULL DEFAULT 'continuous_feedback',
    ADD COLUMN response_blocks jsonb,
    ADD COLUMN localizations jsonb,
    ADD COLUMN allow_comment boolean NOT NULL DEFAULT false,
    ADD COLUMN collect_contact boolean NOT NULL DEFAULT false;

UPDATE widget_rules
SET definition_key = 'widget-' || substr(replace(id::text, '-', ''), 1, 12),
    definition_status = CASE WHEN enabled THEN 'active' ELSE 'paused' END::widget_definition_status,
    presentation = 'prompt',
    response_blocks =
        CASE WHEN can_rate THEN
            jsonb_build_array(jsonb_build_object(
                'key', 'rating', 'type', 'rating', 'required', false
            ))
        ELSE '[]'::jsonb END
        ||
        CASE WHEN can_write_text THEN
            jsonb_build_array(jsonb_build_object(
                'key', 'feedback', 'type', 'free_text', 'required', false
            ))
        ELSE '[]'::jsonb END,
    localizations = jsonb_build_object(
        'en', jsonb_build_object(
            'launcher_label', 'Feedback',
            'prompt', COALESCE(greeting, 'How was your experience?'),
            'comment_placeholder', 'Tell us more',
            'contact_label', 'Email address',
            'consent_label', 'Email me about this feedback',
            'submit_label', 'Send feedback',
            'success_message', 'Your feedback was sent.'
        )
    );

ALTER TABLE widget_rules
    ALTER COLUMN definition_key SET NOT NULL,
    ALTER COLUMN definition_key SET DEFAULT (
        'widget-' || substr(replace(gen_random_uuid()::text, '-', ''), 1, 12)
    ),
    ALTER COLUMN definition_status SET NOT NULL,
    ALTER COLUMN definition_status SET DEFAULT 'active',
    ALTER COLUMN presentation SET NOT NULL,
    ALTER COLUMN presentation SET DEFAULT 'launcher',
    ALTER COLUMN response_blocks SET NOT NULL,
    ALTER COLUMN response_blocks SET DEFAULT '[
        {"key":"rating","type":"rating","required":false},
        {"key":"feedback","type":"free_text","required":false}
    ]'::jsonb,
    ALTER COLUMN localizations SET NOT NULL,
    ALTER COLUMN localizations SET DEFAULT '{
        "en": {
            "launcher_label": "Feedback",
            "prompt": "How was your experience?",
            "comment_placeholder": "Tell us more",
            "contact_label": "Email address",
            "consent_label": "Email me about this feedback",
            "submit_label": "Send feedback",
            "success_message": "Your feedback was sent."
        }
    }'::jsonb,
    ADD CONSTRAINT widget_rules_definition_key_valid CHECK (
        definition_key ~ '^[a-z][a-z0-9-]{1,62}[a-z0-9]$'
    ),
    ADD CONSTRAINT widget_rules_definition_version_valid CHECK (definition_version >= 1),
    ADD CONSTRAINT widget_rules_response_blocks_array CHECK (jsonb_typeof(response_blocks) = 'array'),
    ADD CONSTRAINT widget_rules_localizations_object CHECK (jsonb_typeof(localizations) = 'object'),
    ADD CONSTRAINT widget_rules_project_definition_key_key UNIQUE (project_id, definition_key);

GRANT USAGE ON TYPE widget_definition_status, widget_presentation, widget_objective
    TO fidibeki_app, fidibeki_admin;

