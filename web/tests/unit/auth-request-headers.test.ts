import { describe, expect, it } from 'vitest'
import { authForwardedHeaderNames } from '../../utils/auth-request-headers'

describe('auth request headers', () => {
  it('preserves the browser fingerprint during server-side refreshes', () => {
    expect(authForwardedHeaderNames).toEqual(['cookie', 'host', 'user-agent'])
  })
})
