UPDATE widget_rules SET replay_mask_all_inputs = true WHERE replay_mask_all_inputs = false;

ALTER TABLE widget_rules
    ALTER COLUMN replay_mask_all_inputs SET DEFAULT true,
    ADD CONSTRAINT widget_rules_replay_mask_all_inputs_check
        CHECK (replay_mask_all_inputs = true);
