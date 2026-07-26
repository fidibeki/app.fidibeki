-- Who watched the video, and when.
--
-- Publishing a testimonial publishes its words, which someone read before
-- approving. A video carries everything the words do and everything else that
-- was in frame: a face, a room, a whiteboard, whoever walked past. Approving the
-- quote is not consent to broadcast the footage, so the two are recorded apart.
--
-- Until this column existed LoadWallCards hardcoded VideoReviewed = false, which
-- made a wall containing video unpublishable. That was the safe direction to be
-- wrong in, and this is what makes it true instead.
ALTER TABLE testimonial_media
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN reviewed_by uuid,
    -- Same pairing the approval columns use: a review without a reviewer is not
    -- a review, and the schema refuses to record half of one.
    ADD CONSTRAINT testimonial_media_review_recorded_together
        CHECK ((reviewed_at IS NULL) = (reviewed_by IS NULL)),
    ADD CONSTRAINT testimonial_media_tenant_reviewer_fkey
        FOREIGN KEY (tenant_id, reviewed_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (reviewed_by);

COMMENT ON COLUMN testimonial_media.reviewed_at IS
    'When a member watched this asset and cleared it for publication.';
