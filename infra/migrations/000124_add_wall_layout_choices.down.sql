-- Walls that chose one of the new arrangements fall back to the grid, which is
-- the only value guaranteed to exist once this migration is undone.
UPDATE testimonial_walls SET layout = 'grid' WHERE layout IN ('marquee', 'custom');

-- PostgreSQL enum values cannot be removed safely in a reversible migration.
-- The testimonial_wall_layout type is dropped by migration 000120 during a full
-- rollback, and the forward migration is idempotent.
