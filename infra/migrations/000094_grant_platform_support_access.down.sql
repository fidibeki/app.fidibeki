UPDATE platform_admins
SET permissions = array_remove(permissions, 'platform:tenant:support_access');
