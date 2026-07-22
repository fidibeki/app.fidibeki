ALTER TABLE widget_rules
    DROP CONSTRAINT widget_rules_replay_max_duration_seconds_check,
    DROP CONSTRAINT widget_rules_replay_max_events_check,
    ADD CONSTRAINT widget_rules_replay_max_duration_seconds_check
        CHECK (replay_max_duration_seconds BETWEEN 1 AND 7200),
    ADD CONSTRAINT widget_rules_replay_max_events_check
        CHECK (replay_max_events BETWEEN 1 AND 200000);
