ALTER TABLE widget_rules
    ADD CONSTRAINT widget_rules_replay_max_duration_seconds_v43_check
        CHECK (replay_max_duration_seconds BETWEEN 1 AND 1800) NOT VALID,
    ADD CONSTRAINT widget_rules_replay_max_events_v43_check
        CHECK (replay_max_events BETWEEN 1 AND 50000) NOT VALID;

UPDATE widget_rules
SET replay_max_duration_seconds = LEAST(replay_max_duration_seconds, 1800),
    replay_max_events = LEAST(replay_max_events, 50000)
WHERE replay_max_duration_seconds > 1800 OR replay_max_events > 50000;

ALTER TABLE widget_rules
    VALIDATE CONSTRAINT widget_rules_replay_max_duration_seconds_v43_check;

ALTER TABLE widget_rules
    VALIDATE CONSTRAINT widget_rules_replay_max_events_v43_check;

ALTER TABLE widget_rules
    DROP CONSTRAINT widget_rules_replay_max_duration_seconds_check,
    DROP CONSTRAINT widget_rules_replay_max_events_check;

ALTER TABLE widget_rules
    RENAME CONSTRAINT widget_rules_replay_max_duration_seconds_v43_check
    TO widget_rules_replay_max_duration_seconds_check;

ALTER TABLE widget_rules
    RENAME CONSTRAINT widget_rules_replay_max_events_v43_check
    TO widget_rules_replay_max_events_check;
