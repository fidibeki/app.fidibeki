UPDATE platform_admins SET permissions = array_remove(array_remove(permissions,
    'platform:support:read'), 'platform:support:write');
DELETE FROM retention_maintenance_status WHERE operation_key='support_conversations';
DROP TABLE support_messages;
DROP TABLE support_conversations;
-- Keep the additive audit path format: the append-only ledger can contain
-- support access events after a rollback and must preserve their provenance.
