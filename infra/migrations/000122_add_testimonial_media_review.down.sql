ALTER TABLE testimonial_media
    DROP CONSTRAINT IF EXISTS testimonial_media_tenant_reviewer_fkey,
    DROP CONSTRAINT IF EXISTS testimonial_media_review_recorded_together,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_at;
