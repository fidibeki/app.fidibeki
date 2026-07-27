import { describe, expect, it } from 'vitest'
import { isBackofficeProxyPath } from '~/server/utils/proxy-path'

describe('isBackofficeProxyPath', () => {
  it('only permits platform-admin and existing operations paths', () => {
    expect(isBackofficeProxyPath('/admin/auth/refresh')).toBe(true)
    expect(isBackofficeProxyPath('/admin/backoffice/overview')).toBe(true)
    expect(isBackofficeProxyPath('/admin/queues/')).toBe(true)
    expect(isBackofficeProxyPath('/admin/operations/privacy-incidents')).toBe(true)
    expect(isBackofficeProxyPath('/v1/feedbacks')).toBe(false)
    expect(isBackofficeProxyPath('/auth/refresh')).toBe(false)
    expect(isBackofficeProxyPath('/admin/tenants')).toBe(true)
  })
})
