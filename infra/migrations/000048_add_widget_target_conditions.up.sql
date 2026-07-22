CREATE TYPE widget_target_condition_kind AS ENUM ('include', 'exclude');

CREATE TABLE widget_rule_target_conditions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    widget_rule_id uuid NOT NULL REFERENCES widget_rules(id) ON DELETE CASCADE,
    kind widget_target_condition_kind NOT NULL,
    matcher_type matcher_type NOT NULL,
    matcher_value text,
    position integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT widget_rule_target_conditions_matcher_consistent CHECK (
        (matcher_type = 'all_pages' AND matcher_value IS NULL)
        OR (matcher_type <> 'all_pages' AND matcher_value IS NOT NULL AND matcher_value <> '')
    ),
    CONSTRAINT widget_rule_target_conditions_position_valid CHECK (position >= 0),
    CONSTRAINT widget_rule_target_conditions_position_unique
        UNIQUE (widget_rule_id, kind, position)
);

CREATE INDEX idx_widget_rule_target_conditions_rule
    ON widget_rule_target_conditions (widget_rule_id, kind, position);

CREATE TRIGGER widget_rule_target_conditions_set_updated_at
BEFORE UPDATE ON widget_rule_target_conditions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE widget_rule_target_conditions ENABLE ROW LEVEL SECURITY;
ALTER TABLE widget_rule_target_conditions FORCE ROW LEVEL SECURITY;

CREATE POLICY widget_rule_target_conditions_tenant_isolation ON widget_rule_target_conditions
    FOR ALL TO fidibeki_app
    USING (EXISTS (
        SELECT 1
        FROM widget_rules AS rule
        JOIN projects AS project ON project.id = rule.project_id
        WHERE rule.id = widget_rule_target_conditions.widget_rule_id
          AND project.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    ))
    WITH CHECK (EXISTS (
        SELECT 1
        FROM widget_rules AS rule
        JOIN projects AS project ON project.id = rule.project_id
        WHERE rule.id = widget_rule_target_conditions.widget_rule_id
          AND project.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    ));

WITH parsed_legacy_rules AS (
    SELECT
        rule.id,
        rule.matcher_type,
        rule.matcher_value,
        regexp_match(
            rule.matcher_value,
            '^([Hh][Tt][Tt][Pp][Ss]?)://([^/?#]+)(/[^?#]*)?([?#].*)?$'
        ) AS url_parts
    FROM widget_rules AS rule
), normalized_legacy_rules AS (
    SELECT
        id,
        matcher_type,
        CASE
            WHEN matcher_type IN ('url_exact', 'url_prefix') AND url_parts IS NOT NULL THEN
                lower(url_parts[1]) || '://' ||
                CASE
                    WHEN lower(url_parts[1]) = 'http' THEN regexp_replace(lower(url_parts[2]), ':80$', '')
                    WHEN lower(url_parts[1]) = 'https' THEN regexp_replace(lower(url_parts[2]), ':443$', '')
                    ELSE lower(url_parts[2])
                END ||
                CASE
                    WHEN url_parts[3] IS NULL OR regexp_replace(url_parts[3], '/+$', '') = '' THEN '/'
                    ELSE regexp_replace(url_parts[3], '/+$', '')
                END
            ELSE matcher_value
        END AS matcher_value
    FROM parsed_legacy_rules
)
INSERT INTO widget_rule_target_conditions (
    widget_rule_id, kind, matcher_type, matcher_value, position
)
SELECT id, 'include', matcher_type, matcher_value, 0
FROM normalized_legacy_rules;

GRANT USAGE ON TYPE widget_target_condition_kind TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON widget_rule_target_conditions TO fidibeki_app, fidibeki_admin;
