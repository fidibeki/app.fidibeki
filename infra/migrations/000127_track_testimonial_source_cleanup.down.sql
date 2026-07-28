DROP INDEX IF EXISTS idx_testimonial_media_active_source;
DROP INDEX IF EXISTS idx_testimonial_media_source_cleanup;
DROP INDEX IF EXISTS idx_testimonial_media_abandoned_upload;

ALTER TABLE testimonial_media
    DROP COLUMN IF EXISTS source_deleted_at;
