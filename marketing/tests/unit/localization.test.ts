import { describe, expect, it } from 'vitest'
import en from '../../i18n/locales/en.json'
import pt from '../../i18n/locales/pt.json'
import es from '../../i18n/locales/es.json'
import fr from '../../i18n/locales/fr.json'
import {
  localizedPath,
  markdownPath,
  messageKey,
  unlocalizedPath,
} from '../../utils/locale'

const tokens = (message: string) =>
  [...message.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()

describe('marketing localization', () => {
  for (const [locale, catalog] of Object.entries({ pt, es, fr })) {
    it(`${locale} covers every message and preserves interpolation parameters`, () => {
      expect(Object.keys(catalog).sort()).toEqual(Object.keys(en).sort())
      for (const [key, value] of Object.entries(en)) {
        const translated = catalog[key as keyof typeof catalog]
        expect(translated.trim(), key).not.toBe('')
        expect(tokens(translated), `${locale} ${value}`).toEqual(tokens(value))
      }
    })
  }
  it('has stable source keys, including escaped message syntax', () => {
    for (const [key, value] of Object.entries(en)) {
      expect(messageKey(value.replace(/\{'([@<>|])'\}/g, '$1')), value).toBe(
        key,
      )
    }
  })
  it('keeps locale changes on the same path with query and hash', () => {
    expect(localizedPath('/fr/docs/installation?ref=nav#domains', 'pt')).toBe(
      '/pt/docs/installation?ref=nav#domains',
    )
    expect(localizedPath('/pt?ref=nav#demo', 'en')).toBe('/?ref=nav#demo')
    expect(localizedPath('/?ref=nav#demo', 'fr')).toBe('/fr?ref=nav#demo')
    expect(unlocalizedPath('/pricing')).toBe('/pricing')
    expect(localizedPath('https://app.example.com/login', 'pt')).toBe(
      'https://app.example.com/login',
    )
    expect(markdownPath('/pt')).toBe('/pt/index.md')
    expect(markdownPath('/docs/installation')).toBe('/docs/installation.md')
  })
})
