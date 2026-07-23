-- The backfill is intentionally not reversed: input masking and canvas suppression are
-- anonymity-safe defaults (issue #53), not customer-reversible state.
ALTER TABLE widget_rules
    ALTER COLUMN replay_mask_all_inputs SET DEFAULT true,
    DROP CONSTRAINT widget_rules_replay_mask_all_inputs_check,
    ALTER COLUMN replay_record_canvas SET DEFAULT false,
    DROP CONSTRAINT widget_rules_replay_record_canvas_check;
