CREATE TYPE widget_recurrence_mode AS ENUM ('cooldown', 'session', 'never');

ALTER TABLE widget_rules
    ADD COLUMN recurrence_mode widget_recurrence_mode NOT NULL DEFAULT 'cooldown',
    ADD COLUMN recurrence_days integer DEFAULT 30,
    ADD CONSTRAINT widget_rules_recurrence_consistent CHECK (
        (recurrence_mode = 'cooldown' AND recurrence_days BETWEEN 1 AND 3650)
        OR (recurrence_mode <> 'cooldown' AND recurrence_days IS NULL)
    );

GRANT USAGE ON TYPE widget_recurrence_mode TO fidibeki_app, fidibeki_admin;
