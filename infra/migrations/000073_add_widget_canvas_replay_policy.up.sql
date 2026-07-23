ALTER TABLE widget_rules
    ADD COLUMN replay_record_canvas boolean NOT NULL DEFAULT false,
    ADD COLUMN replay_canvas_fps integer NOT NULL DEFAULT 2,
    ADD CONSTRAINT widget_rules_replay_canvas_fps_check
        CHECK (replay_canvas_fps BETWEEN 1 AND 5);
