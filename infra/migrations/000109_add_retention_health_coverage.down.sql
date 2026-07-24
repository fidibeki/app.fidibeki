DELETE FROM retention_maintenance_status
WHERE operation_key NOT IN ('tenant_magic_links', 'platform_admin_magic_links');

ALTER TABLE retention_maintenance_status
    DROP CONSTRAINT retention_maintenance_status_operation_key_check,
    DROP CONSTRAINT retention_maintenance_status_category_check,
    ADD CONSTRAINT retention_maintenance_status_operation_key_check
        CHECK (operation_key IN (
            'tenant_magic_links',
            'platform_admin_magic_links'
        )),
    ADD CONSTRAINT retention_maintenance_status_category_check
        CHECK (category = 'auth_magic_links');
