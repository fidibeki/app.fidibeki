import { describe, expect, it } from 'vitest'
import {
  applicationURL,
  baseDomainURL,
  isApplicationHost,
  isBaseDomainHost,
  normalizeWorkspaceSlug,
  workspaceLoginURL,
  workspaceSlugFromStandardHost,
  workspaceSlugError,
} from '../../utils/workspace-url'

describe('workspace URL helpers', () => {
  it('normalizes common workspace names without accepting invalid output', () => {
    expect(normalizeWorkspaceSlug('  Acme Design Studio  ')).toBe('acme-design-studio')
    expect(normalizeWorkspaceSlug('Acme___Store')).toBe('acme-store')
    expect(workspaceSlugError('acme-store')).toBe('')
    expect(workspaceSlugError('api')).not.toBe('')
    expect(workspaceSlugError('-acme')).not.toBe('')
  })

  it('builds environment-correct public and tenant URLs', () => {
    const staging = new URL('https://staging.example.test/signup')
    expect(baseDomainURL('staging.example.test', '/signup', staging))
      .toBe('https://staging.example.test/signup')
    expect(workspaceLoginURL('acme', 'staging.example.test', staging))
      .toBe('https://acme.staging.example.test/login')

    const local = new URL('http://acme.localhost:4321/login')
    expect(baseDomainURL('localhost', '/signup', local)).toBe('http://localhost:4321/signup')
    expect(workspaceLoginURL('acme', 'localhost', local)).toBe('http://acme.localhost:4321/login')
  })

  it('distinguishes the environment root from tenant hosts', () => {
    expect(isBaseDomainHost('staging.example.test', 'staging.example.test')).toBe(true)
    expect(isBaseDomainHost('acme.staging.example.test', 'staging.example.test')).toBe(false)
  })

  it('supports an application entry origin separate from the tenant base domain', () => {
    const tenantURL = new URL('https://acme.fidibeki.com/login')

    expect(applicationURL('https://app.fidibeki.com', 'fidibeki.com', '/signup', tenantURL))
      .toBe('https://app.fidibeki.com/signup')
    expect(isApplicationHost('app.fidibeki.com', 'fidibeki.com', 'https://app.fidibeki.com'))
      .toBe(true)
    expect(isApplicationHost('fidibeki.com', 'fidibeki.com', 'https://app.fidibeki.com'))
      .toBe(true)
    expect(isApplicationHost('acme.fidibeki.com', 'fidibeki.com', 'https://app.fidibeki.com'))
      .toBe(false)
  })

  it('suggests a slug only for a valid direct subdomain of the configured base domain', () => {
    expect(workspaceSlugFromStandardHost('acme.staging.example.test', 'staging.example.test')).toBe('acme')
    expect(workspaceSlugFromStandardHost('acme.localhost', 'localhost')).toBe('acme')
    expect(workspaceSlugFromStandardHost('custom.example.com', 'staging.example.test')).toBeNull()
    expect(workspaceSlugFromStandardHost('one.two.staging.example.test', 'staging.example.test')).toBeNull()
    expect(workspaceSlugFromStandardHost('api.staging.example.test', 'staging.example.test')).toBeNull()
    expect(workspaceSlugFromStandardHost('staging.example.test', 'staging.example.test')).toBeNull()
  })
})
