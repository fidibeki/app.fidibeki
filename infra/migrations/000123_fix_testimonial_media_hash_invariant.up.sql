-- The content hash belongs to being servable, not to having arrived.
--
-- 000119 tied it to completed_at, which cannot hold: completion means the bytes
-- landed in object storage and nothing has read them yet, so nothing can know
-- their digest without a full read on the request path — for a phone video, on
-- the API, while the author waits.
--
-- What the hash is actually for is the public media path, and that path is only
-- ever built from an asset that finished transcoding. So the invariant is that
-- anything servable is addressable, which is what this states instead.
ALTER TABLE testimonial_media
    DROP CONSTRAINT testimonial_media_completed_has_hash;

ALTER TABLE testimonial_media
    ADD CONSTRAINT testimonial_media_ready_has_hash CHECK (
        transcode_status <> 'ready' OR content_hash IS NOT NULL
    );
