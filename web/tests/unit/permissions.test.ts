import { describe, expect, it } from 'vitest'
import {
  allTenantPermissions,
  permissionPresets,
  tenantPermissionGroups,
} from '../../data/permissions'

describe('tenant permission catalog', () => {
  it('exposes service-to-service capture permissions without duplicates', () => {
    expect(allTenantPermissions).toContain('mobile_capture:create')
    expect(allTenantPermissions).toContain('email_feedback:send')
    expect(new Set(allTenantPermissions).size).toBe(allTenantPermissions.length)
    expect(permissionPresets.admin).toEqual(allTenantPermissions)

    const emailGroup = tenantPermissionGroups.find(
      group => group.permissions.some(permission => permission === 'email_feedback:send'),
    )
    expect(emailGroup?.permissions).toEqual(['email_feedback:send'])
  })
})
