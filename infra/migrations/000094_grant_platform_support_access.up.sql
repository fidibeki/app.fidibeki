-- Backfill the support permission only for existing administrators who can
-- already manage platform-admin permissions. Read-only or otherwise limited
-- platform admins must receive this sensitive-data capability explicitly.
UPDATE platform_admins
SET permissions = array_append(permissions, 'platform:tenant:support_access')
WHERE 'platform:admin:update' = ANY(permissions)
  AND NOT ('platform:tenant:support_access' = ANY(permissions));
