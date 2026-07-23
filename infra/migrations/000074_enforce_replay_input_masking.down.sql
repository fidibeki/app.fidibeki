-- The backfill of previously-false rows is intentionally not reversed: replay input masking
-- is an anonymity-safe default (issue #53), not customer-reversible state.
ALTER TABLE widget_rules
    ALTER COLUMN replay_mask_all_inputs SET DEFAULT true,
    DROP CONSTRAINT widget_rules_replay_mask_all_inputs_check;
