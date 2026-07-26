ALTER TABLE testimonial_media
    DROP CONSTRAINT IF EXISTS testimonial_media_ready_has_hash;

ALTER TABLE testimonial_media
    ADD CONSTRAINT testimonial_media_completed_has_hash CHECK (
        completed_at IS NULL OR content_hash IS NOT NULL
    );
