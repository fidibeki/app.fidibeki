ALTER TABLE testimonial_media
    ADD COLUMN source_deleted_at timestamptz;

CREATE INDEX idx_testimonial_media_abandoned_upload
    ON testimonial_media (created_at)
    WHERE role = 'video' AND completed_at IS NULL AND source_deleted_at IS NULL;

CREATE INDEX idx_testimonial_media_source_cleanup
    ON testimonial_media (updated_at)
    WHERE role = 'video'
      AND completed_at IS NOT NULL
      AND source_deleted_at IS NULL
      AND transcode_status IN ('ready', 'failed');

CREATE INDEX idx_testimonial_media_active_source
    ON testimonial_media (source_storage_key)
    WHERE source_deleted_at IS NULL;
