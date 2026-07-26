-- Backfill testimonial permissions for existing active members.
--
-- testimonial:read/create/update/delete/publish were added to AllTenantPermissions
-- when the testimonials feature landed, so new members receive them via
-- TenantPermissionStrings(). Existing members were created before that and
-- carry the older permission set from migration 000060, so the Testimonials
-- nav item and the Testimonials permission group in Settings stay hidden for
-- them even on plans that include the feature. Grant the full set to every
-- active member so the workspace behaves the same regardless of when the
-- tenant was created. Bump authorization_version to invalidate existing
-- sessions so the new permissions are picked up on the next request rather
-- than after a manual re-login.

UPDATE tenant_members
SET permissions = permissions
        || ARRAY[
            'testimonial:read',
            'testimonial:create',
            'testimonial:update',
            'testimonial:delete',
            'testimonial:publish'
        ]::text[],
    authorization_version = authorization_version + 1
WHERE status = 'active'
  AND NOT ('testimonial:read' = ANY(permissions));