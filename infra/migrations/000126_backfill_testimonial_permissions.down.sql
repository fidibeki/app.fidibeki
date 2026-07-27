-- Revoke the testimonial permissions that migration 000126 backfilled.
-- New tenants created after 000126 still receive them via
-- TenantPermissionStrings(); this only strips the backfilled grants from
-- members who had no testimonial permissions before. authorization_version is
-- bumped so existing sessions reflect the change on the next request.

UPDATE tenant_members
SET permissions = array_remove(
        array_remove(
            array_remove(
                array_remove(
                    array_remove(permissions, 'testimonial:read'),
                    'testimonial:create'),
                'testimonial:update'),
            'testimonial:delete'),
        'testimonial:publish'),
    authorization_version = authorization_version + 1
WHERE status = 'active';