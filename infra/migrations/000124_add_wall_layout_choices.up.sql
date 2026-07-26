-- Two more ways to arrange a wall.
--
-- 'marquee' is a row that scrolls by itself, the shape most people picture when
-- they say "wall of love". 'custom' is not an arrangement at all: it records
-- that this wall is consumed through the public JSON endpoint and rendered by
-- the customer's own code, so the screen offers them the API instead of an
-- embed snippet. Storing it makes that a property of the wall rather than a
-- preference living in someone's head.
ALTER TYPE testimonial_wall_layout ADD VALUE IF NOT EXISTS 'marquee' AFTER 'carousel';
ALTER TYPE testimonial_wall_layout ADD VALUE IF NOT EXISTS 'custom';
