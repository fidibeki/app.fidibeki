export function identityInitials(name?: string | null, email?: string | null): string {
  const source = name?.trim() || email?.split('@')[0]?.trim() || '?'
  const parts = source.split(/\s+/).filter(Boolean)
  const initials = parts.length > 1
    ? `${parts[0]?.[0] || ''}${parts.at(-1)?.[0] || ''}`
    : source.slice(0, 2)
  return initials.toLocaleUpperCase()
}

export function workspaceLogoURL(
  tenantID: string,
  version?: string | null,
  size: 64 | 256 = 64,
): string | null {
  if (!tenantID || !version) return null
  return `/v1/public/workspace-logo/${encodeURIComponent(tenantID)}/${encodeURIComponent(version)}/${size}.png`
}

export function memberAvatarURL(
  memberID: string,
  version?: string | null,
  size: 64 | 256 = 64,
): string | null {
  if (!memberID || !version) return null
  return `/v1/members/${encodeURIComponent(memberID)}/avatar/${encodeURIComponent(version)}/${size}.png`
}
