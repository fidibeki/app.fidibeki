import { describe, expect, it } from 'vitest'
import { proxyHostForRequest } from '../../server/utils/proxy-host'

describe('proxyHostForRequest', () => {
  it('uses the trusted platform host for platform-scoped public requests', () => {
    expect(proxyHostForRequest('GET', '/v1/plans', 'staging.example.test', 'staging.example.test'))
      .toBe('api.staging.example.test')
    expect(proxyHostForRequest('POST', '/v1/signup', 'staging.example.test', 'staging.example.test'))
      .toBe('api.staging.example.test')
    expect(proxyHostForRequest(
      'GET',
      '/v1/public/workspace-logo/tenant-id/version/64.png',
      'acme.staging.example.test',
      'staging.example.test',
    )).toBe('api.staging.example.test')
  })

  it('preserves tenant routing for every other method and path', () => {
    expect(proxyHostForRequest('GET', '/v1/projects', 'acme.staging.example.test', 'staging.example.test'))
      .toBe('acme.staging.example.test')
    expect(proxyHostForRequest('GET', '/v1/signup', 'acme.staging.example.test', 'staging.example.test'))
      .toBe('acme.staging.example.test')
    expect(proxyHostForRequest('POST', '/v1/plans', 'acme.staging.example.test', 'staging.example.test'))
      .toBe('acme.staging.example.test')
    expect(proxyHostForRequest(
      'POST',
      '/v1/public/workspace-logo/tenant-id/version/64.png',
      'acme.staging.example.test',
      'staging.example.test',
    )).toBe('acme.staging.example.test')
    expect(proxyHostForRequest(
      'GET',
      '/v1/public/workspace-logos',
      'acme.staging.example.test',
      'staging.example.test',
    )).toBe('acme.staging.example.test')
  })
})
