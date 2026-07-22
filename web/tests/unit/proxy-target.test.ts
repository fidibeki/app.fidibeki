import { describe, expect, it } from 'vitest'
import { proxyTargetForRequest } from '../../server/utils/proxy-target'

describe('proxyTargetForRequest', () => {
  it('keeps the configured upstream when the request uses an absolute-form URL', () => {
    const target = proxyTargetForRequest(
      'https://api.internal.example',
      new URL('https://attacker.example/v1/projects?status=open'),
    )

    expect(target.toString()).toBe('https://api.internal.example/v1/projects?status=open')
  })

  it('rejects an upstream that cannot safely receive the internal proxy secret', () => {
    expect(() => proxyTargetForRequest(
      'file:///tmp/capture',
      new URL('https://tenant.example/v1/projects'),
    )).toThrow('API upstream must use HTTP or HTTPS')
  })

  it('rejects a malformed configured upstream', () => {
    expect(() => proxyTargetForRequest(
      'not a URL',
      new URL('https://tenant.example/v1/projects'),
    )).toThrow()
  })
})
