-- Support the bounded, batched expiry sweep added for issue #62: magic links
-- and invitations (tenant_magic_links rows with member_id IS NULL) already
-- carry an authoritative expires_at boundary enforced by ConsumeMagicLink;
-- this index lets the retention maintainer scan for expired rows without a
-- sequential scan as the table grows. No existing row's expires_at changes,
-- so no retention clock resets for legacy data.
CREATE INDEX idx_tenant_magic_links_expires_at ON tenant_magic_links (expires_at);
