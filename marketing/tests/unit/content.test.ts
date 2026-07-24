import { describe, expect, it } from 'vitest'
import { features, featureSlugs } from '~/data/features'
import { integrations, integrationSlugs } from '~/data/integrations'
import { useCases, useCaseSlugs } from '~/data/use-cases'
import { docs, docSlugs } from '~/data/docs'
import { plans } from '~/data/pricing'
import { customers, footerGroups, marketingRoutes, primaryNav } from '~/data/site'

function assertUnique(values: string[], label: string) {
  const duplicates = values.filter((value, index) => values.indexOf(value) !== index)
  expect(duplicates, `${label} must be unique`).toEqual([])
}

describe('content integrity', () => {
  it('has unique slugs everywhere', () => {
    assertUnique(featureSlugs, 'feature slugs')
    assertUnique(integrationSlugs, 'integration slugs')
    assertUnique(useCaseSlugs, 'use case slugs')
    assertUnique(docSlugs, 'doc slugs')
    assertUnique(marketingRoutes, 'marketing routes')
  })

  it('uses URL-safe slugs', () => {
    const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
    for (const slug of [...featureSlugs, ...integrationSlugs, ...useCaseSlugs, ...docSlugs]) {
      expect(slug).toMatch(slugPattern)
    }
  })

  it('fills every required feature field', () => {
    for (const feature of features) {
      expect(feature.name.length).toBeGreaterThan(2)
      expect(feature.tagline.length).toBeGreaterThan(10)
      expect(feature.summary.length).toBeGreaterThan(30)
      expect(feature.highlights.length).toBeGreaterThanOrEqual(3)
      expect(feature.sections.length).toBeGreaterThanOrEqual(2)
      expect(feature.availability.length).toBeGreaterThan(3)
    }
  })

  it('keeps feature cross-references valid', () => {
    for (const feature of features) {
      for (const related of feature.related) {
        expect(featureSlugs, `${feature.slug} references ${related}`).toContain(related)
        expect(related).not.toBe(feature.slug)
      }
    }
  })

  it('keeps use-case feature references valid', () => {
    for (const useCase of useCases) {
      expect(useCase.featureSlugs.length).toBeGreaterThanOrEqual(2)
      for (const slug of useCase.featureSlugs) {
        expect(featureSlugs, `${useCase.slug} references ${slug}`).toContain(slug)
      }
    }
  })

  it('marks integration status explicitly', () => {
    for (const integration of integrations) {
      expect(['available', 'planned']).toContain(integration.status)
      expect(integration.sections.length).toBeGreaterThanOrEqual(1)
    }
    const available = integrations.filter((entry) => entry.status === 'available')
    expect(available.map((entry) => entry.slug).sort()).toEqual([
      'microsoft-teams',
      'slack',
      'webhooks',
    ])
  })

  it('only lists the four real destination events', () => {
    const expected = ['feedback.created', 'issue.created', 'issue.updated', 'issue.resolved']
    for (const integration of integrations) {
      if (integration.events) {
        expect(integration.events).toEqual(expected)
      }
    }
  })

  it('routes every nav and footer link to a known marketing route', () => {
    const known = new Set(marketingRoutes)
    for (const link of primaryNav) {
      expect(known, `nav link ${link.to}`).toContain(link.to)
    }
    for (const group of footerGroups) {
      for (const link of group.links) {
        expect(known, `footer link ${link.to}`).toContain(link.to)
      }
    }
  })

  it('derives detail routes from content slugs', () => {
    for (const slug of featureSlugs) {
      expect(marketingRoutes).toContain(`/features/${slug}`)
    }
    for (const slug of integrationSlugs) {
      expect(marketingRoutes).toContain(`/integrations/${slug}`)
    }
    for (const slug of useCaseSlugs) {
      expect(marketingRoutes).toContain(`/use-cases/${slug}`)
    }
    for (const slug of docSlugs) {
      expect(marketingRoutes).toContain(`/docs/${slug}`)
    }
  })

  it('includes all pages required by the information architecture', () => {
    for (const route of [
      '/',
      '/features',
      '/integrations',
      '/pricing',
      '/docs',
      '/use-cases',
      '/privacy',
      '/security',
      '/sign-up',
    ]) {
      expect(marketingRoutes).toContain(route)
    }
  })

  it('only claims available integrations that ship today', () => {
    const forbidden = /\b(github|jira|linear|intercom|zapier|discord)\b/i
    for (const integration of integrations) {
      if (integration.status === 'available') {
        expect(integration.name).not.toMatch(forbidden)
      }
    }
    const allowedPlanned = new Set(['jira', 'linear'])
    for (const integration of integrations) {
      if (integration.status === 'planned') {
        expect(['jira', 'linear', 'slack-actions']).toContain(integration.slug)
      }
    }
  })

  it('documents the real embed snippet', () => {
    const installation = docs.find((doc) => doc.slug === 'installation')
    expect(installation).toBeDefined()
    const code = installation!.sections.flatMap((section) => section.code?.snippet ?? [])
    expect(code.join('\n')).toContain('widget.fidibeki.com/v1/loader.js')
    expect(code.join('\n')).toContain('data-fidibeki-key')
  })

  it('lists only complete customer entries', () => {
    expect(customers.length).toBeGreaterThan(0)
    for (const customer of customers) {
      expect(customer.name.length).toBeGreaterThan(1)
      expect(customer.url).toMatch(/^https:\/\//)
      expect(customer.logoLight).toMatch(/^\/customers\//)
      expect(customer.logoDark).toMatch(/^\/customers\//)
    }
  })

  it('has exactly four plans in ascending price order', () => {
    expect(plans.map((plan) => plan.id)).toEqual(['starter', 'growth', 'scale', 'enterprise'])
    const priced = plans.filter((plan) => plan.monthlyPriceCents !== null)
    for (let index = 1; index < priced.length; index += 1) {
      expect(priced[index].monthlyPriceCents!).toBeGreaterThan(priced[index - 1].monthlyPriceCents!)
    }
  })
})
