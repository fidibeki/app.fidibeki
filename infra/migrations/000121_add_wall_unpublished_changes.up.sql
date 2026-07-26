-- Whether the wall's current selection differs from the copy that is live.
--
-- This is a flag rather than a comparison of timestamps because the two
-- timestamps that would be compared come from different clocks: published_at is
-- stamped by the application, updated_at by a database trigger. They agree to
-- within milliseconds in production and not at all under a frozen test clock,
-- which is not a foundation to put a publish button on.
--
-- Nor can it be derived by comparing the selection with the published snapshot:
-- the snapshot deliberately drops cards that are not publishable, so a wall
-- carrying one unpublishable testimonial would compare unequal forever.
--
-- Existing walls start dirty. A wall published before this column existed may
-- genuinely have had its selection edited since, and offering a publish that is
-- not needed is a smaller harm than hiding one that is.
ALTER TABLE testimonial_walls
    ADD COLUMN has_unpublished_changes boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN testimonial_walls.has_unpublished_changes IS
    'Set when the selection or presentation changes, cleared when a snapshot is published.';
