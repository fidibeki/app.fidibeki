DELETE FROM retention_maintenance_status WHERE operation_key='website_chat_conversations';
UPDATE tenant_members SET permissions=array_remove(array_remove(array_remove(permissions,'chat:read'),'chat:write'),'chat:delete');
DROP TABLE website_chat_messages;
DROP TABLE website_chat_visitors;
ALTER TABLE projects DROP COLUMN chat_enabled;
