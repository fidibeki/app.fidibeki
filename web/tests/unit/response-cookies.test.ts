import { describe, expect, it, vi } from 'vitest'
import { forwardResponseCookies } from '../../utils/response-cookies'

describe('forwardResponseCookies', () => {
  it('forwards every upstream Set-Cookie value to the browser response', () => {
    const append = vi.fn()
    const headers = {
      get: () => null,
      getSetCookie: () => [
        'fidibeki_tenant_refresh=next; Path=/; HttpOnly; SameSite=Strict',
        'fidibeki_device=known; Path=/; SameSite=Lax',
      ],
    }

    forwardResponseCookies(headers, append)

    expect(append).toHaveBeenNthCalledWith(1, headers.getSetCookie()[0])
    expect(append).toHaveBeenNthCalledWith(2, headers.getSetCookie()[1])
  })
})
