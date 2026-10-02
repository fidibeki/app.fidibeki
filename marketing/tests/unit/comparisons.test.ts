import { describe, expect, it } from 'vitest'
import {
  competitors,
  comparisonForPath,
  comparisonRoutes,
  alternativePath,
  versusPath,
} from '../../data/comparisons'
import { marketingRoutes } from '../../data/site'
import { featureSlugs } from '../../data/features'
import { messageKey } from '../../utils/locale'
import en from '../../i18n/locales/en.json'
import pt from '../../i18n/locales/pt.json'
import es from '../../i18n/locales/es.json'
import fr from '../../i18n/locales/fr.json'

describe('competitor comparisons', () => {
  it('publishes the eight requested routes and rejects unknown names', () => {
    expect(comparisonRoutes).toHaveLength(8)
    expect(new Set(comparisonRoutes).size).toBe(8)
    for (const competitor of competitors) {
      for (const [path, kind] of [
        [alternativePath(competitor.slug), 'alternative'],
        [versusPath(competitor.slug), 'versus'],
      ] as const) {
        expect(marketingRoutes).toContain(path)
        expect(comparisonForPath(path)).toEqual({ competitor, kind })
      }
    }
    for (const path of [
      '/unknown-alternative',
      '/fidibeki-vs-unknown',
      '/userback-alternative-extra',
      '/compare/userback',
      '/pricing',
    ]) {
      expect(comparisonForPath(path)).toBeUndefined()
    }
  })

  it('keeps comparison evidence and product links traceable', () => {
    for (const competitor of competitors) {
      expect(competitor.reviewedAt).toMatch(/^\d{4}-\d{2}-\d{2}$/)
      const sources = new Set(competitor.sources.map((source) => source.url))
      expect(
        competitor.sources.some((source) => source.label === 'Pricing'),
      ).toBe(true)
      for (const url of sources) expect(new URL(url).protocol).toBe('https:')
      for (const row of competitor.versus.rows) {
        expect(sources, row.topic).toContain(row.source)
        expect(marketingRoutes, row.topic).toContain(row.productPath)
      }
      for (const slug of competitor.featureSlugs)
        expect(featureSlugs).toContain(slug)
    }
  })

  for (const [locale, catalog] of Object.entries({ en, pt, es, fr })) {
    it(`translates every comparison paragraph and table cell in ${locale}`, () => {
      for (const competitor of competitors) {
        const messages = [
          competitor.focus,
          ...competitor.sources.map((source) => source.label),
          ...Object.values(competitor.alternative).flat(),
          competitor.versus.description,
          competitor.versus.verdict,
          competitor.versus.scenario,
          ...competitor.versus.rows.flatMap((row) => [
            row.topic,
            row.fidibeki,
            row.competitor,
            row.detail,
          ]),
          ...competitor.pricing.map((plan) => plan.note),
          competitor.pricingNote,
        ]
        for (const message of messages) {
          const translated = (catalog as Record<string, string>)[
            messageKey(message)
          ]
          expect(translated, `${locale}: ${message}`).toBeTruthy()
          if (locale !== 'en' && message.length > 40)
            expect(translated, `${locale}: ${message}`).not.toBe(message)
        }
      }
    })
  }
})
