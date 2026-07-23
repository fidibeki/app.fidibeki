UPDATE widget_rules SET replay_mask_all_inputs = true WHERE replay_mask_all_inputs = false;
UPDATE widget_rules SET replay_record_canvas = false WHERE replay_record_canvas = true;

ALTER TABLE widget_rules
    ALTER COLUMN replay_mask_all_inputs SET DEFAULT true,
    ADD CONSTRAINT widget_rules_replay_mask_all_inputs_check
        CHECK (replay_mask_all_inputs = true),
    ALTER COLUMN replay_record_canvas SET DEFAULT false,
    ADD CONSTRAINT widget_rules_replay_record_canvas_check
        CHECK (replay_record_canvas = false);
