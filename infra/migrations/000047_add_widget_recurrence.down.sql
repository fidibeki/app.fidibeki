ALTER TABLE widget_rules
    DROP CONSTRAINT widget_rules_recurrence_consistent,
    DROP COLUMN recurrence_days,
    DROP COLUMN recurrence_mode;

DROP TYPE widget_recurrence_mode;
