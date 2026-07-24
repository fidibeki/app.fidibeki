ALTER TABLE widget_rules
    ADD COLUMN launcher_variant text;

UPDATE widget_rules
SET launcher_variant = 'floating_button';

ALTER TABLE widget_rules
    ALTER COLUMN launcher_variant SET NOT NULL,
    ALTER COLUMN launcher_variant SET DEFAULT 'edge_tab',
    ADD CONSTRAINT widget_rules_launcher_variant_valid CHECK (
        launcher_variant IN ('edge_tab', 'floating_button')
    );
