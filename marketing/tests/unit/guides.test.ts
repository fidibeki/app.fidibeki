import { describe, expect, it } from 'vitest'
import { guides } from '../../data/guides'
import { marketingRoutes } from '../../data/site'
import { messageKey } from '../../utils/locale'
import en from '../../i18n/locales/en.json'
import pt from '../../i18n/locales/pt.json'
import es from '../../i18n/locales/es.json'
import fr from '../../i18n/locales/fr.json'

describe('public guides', () => {
  it('keeps every article discoverable and its product references valid', () => {
    expect(marketingRoutes).toContain('/guides')
    for (const guide of guides) {
      expect(marketingRoutes).toContain(`/guides/${guide.slug}`)
      for (const link of guide.related) expect(marketingRoutes).toContain(link.to)
    }
  })
  for (const [locale, catalog] of Object.entries({ en, pt, es, fr })) {
    it(`translates the complete articles and reading links in ${locale}`, () => {
      for (const guide of guides) {
        const messages = [guide.title, guide.description, ...guide.related.map(link => link.label), ...guide.sections.flatMap(section => [section.heading, section.body, ...(section.bullets || [])])]
        for (const message of messages) expect((catalog as Record<string, string>)[messageKey(message)], message).toBeTruthy()
      }
    })
  }
})
