import { describe, expect, it } from 'vitest'
import { proxyHostForRequest } from '../../server/utils/proxy-host'

describe('proxyHostForRequest', () => {
  it('uses the trusted platform host only for the public catalog and signup', () => {
    expect(proxyHostForRequest('GET', '/v1/plans', 'staging.fidibeki.com', 'staging.fidibeki.com'))
      .toBe('api.staging.fidibeki.com')
    expect(proxyHostForRequest('POST', '/v1/signup', 'staging.fidibeki.com', 'staging.fidibeki.com'))
      .toBe('api.staging.fidibeki.com')
  })

  it('preserves tenant routing for every other method and path', () => {
    expect(proxyHostForRequest('GET', '/v1/projects', 'acme.staging.fidibeki.com', 'staging.fidibeki.com'))
      .toBe('acme.staging.fidibeki.com')
    expect(proxyHostForRequest('GET', '/v1/signup', 'acme.staging.fidibeki.com', 'staging.fidibeki.com'))
      .toBe('acme.staging.fidibeki.com')
    expect(proxyHostForRequest('POST', '/v1/plans', 'acme.staging.fidibeki.com', 'staging.fidibeki.com'))
      .toBe('acme.staging.fidibeki.com')
  })
})
