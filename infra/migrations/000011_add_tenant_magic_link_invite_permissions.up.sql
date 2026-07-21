ALTER TABLE tenant_magic_links
    ADD COLUMN invited_permissions text[] NOT NULL DEFAULT '{}';
