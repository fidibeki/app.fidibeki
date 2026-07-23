import { describe, expect, it } from 'vitest'
import { identityInitials, memberAvatarURL, workspaceLogoURL } from '../../utils/identity'

describe('identity helpers', () => {
  it('creates stable initials from names and email fallbacks', () => {
    expect(identityInitials('Jamie Lee Curtis', 'jamie@example.com')).toBe('JC')
    expect(identityInitials('', 'analyst@example.com')).toBe('AN')
    expect(identityInitials()).toBe('?')
  })

  it('builds versioned identity URLs without accepting storage keys', () => {
    expect(workspaceLogoURL('tenant/1', 'logo?2', 256))
      .toBe('/v1/public/workspace-logo/tenant%2F1/logo%3F2/256.png')
    expect(memberAvatarURL('member/1', 'avatar#2'))
      .toBe('/v1/members/member%2F1/avatar/avatar%232/64.png')
    expect(workspaceLogoURL('tenant-1', null)).toBeNull()
    expect(memberAvatarURL('member-1', '')).toBeNull()
  })
})
