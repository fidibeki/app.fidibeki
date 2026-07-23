import { describe, expect, it } from 'vitest'
import {
  localeDirection,
  matchBrowserLocale,
  normalizeSupportedLocale,
  normalizeSupportedTimezone,
  parseAcceptLanguage,
  resolveRegionalPreferences,
} from '../../utils/regional'

describe('regional preference resolution', () => {
  it('normalizes only locales supported by the product', () => {
    expect(normalizeSupportedLocale(' PT_ao ')).toBe('pt-AO')
    expect(normalizeSupportedLocale('pt-PT')).toBe('pt-PT')
    expect(normalizeSupportedLocale('EN_xa')).toBe('en-XA')
    expect(normalizeSupportedLocale('pt-BR')).toBeNull()
    expect(normalizeSupportedLocale('fr-FR')).toBeNull()
    expect(normalizeSupportedLocale(null)).toBeNull()
  })

  it('matches regional browser languages to a supported catalog', () => {
    expect(matchBrowserLocale(['fr-FR', 'pt-BR', 'en-GB'])).toBe('pt')
    expect(matchBrowserLocale(['en-XA', 'pt-PT'])).toBe('en-XA')
    expect(matchBrowserLocale(['fr-FR'])).toBeNull()
  })

  it('parses Accept-Language quality and ignores disabled or wildcard entries', () => {
    expect(parseAcceptLanguage('en-GB;q=0.7, pt-AO, pt;q=0.8, *;q=0.9, fr;q=0'))
      .toEqual(['pt-AO', 'pt', 'en-GB'])
    expect(parseAcceptLanguage(undefined)).toEqual([])
  })

  it('uses member, organization, browser, and default precedence independently', () => {
    expect(resolveRegionalPreferences({
      memberLocale: 'pt-PT',
      memberTimezone: 'Europe/Lisbon',
      tenantLocale: 'pt-AO',
      tenantTimezone: 'Africa/Luanda',
      browserLocales: ['en-US'],
      browserTimezone: 'America/New_York',
    })).toEqual({
      locale: 'pt-PT',
      localeSource: 'member',
      timezone: 'Europe/Lisbon',
      timezoneSource: 'member',
    })

    expect(resolveRegionalPreferences({
      tenantLocale: 'pt-AO',
      tenantTimezone: 'Africa/Luanda',
      browserLocales: ['en-US'],
      browserTimezone: 'America/New_York',
    })).toEqual({
      locale: 'pt-AO',
      localeSource: 'organization',
      timezone: 'Africa/Luanda',
      timezoneSource: 'organization',
    })

    expect(resolveRegionalPreferences({
      memberLocale: 'fr-FR',
      memberTimezone: 'Local',
      tenantLocale: 'es-ES',
      tenantTimezone: 'Nowhere/Unknown',
      browserLocales: ['pt-BR'],
      browserTimezone: 'America/Sao_Paulo',
    })).toEqual({
      locale: 'pt',
      localeSource: 'browser',
      timezone: 'America/Sao_Paulo',
      timezoneSource: 'browser',
    })

    expect(resolveRegionalPreferences({})).toEqual({
      locale: 'en',
      localeSource: 'default',
      timezone: 'UTC',
      timezoneSource: 'default',
    })
  })

  it('accepts IANA timezones and keeps the current locale set LTR', () => {
    expect(normalizeSupportedTimezone(' Africa/Luanda ')).toBe('Africa/Luanda')
    expect(normalizeSupportedTimezone('UTC')).toBe('UTC')
    expect(normalizeSupportedTimezone('Local')).toBeNull()
    expect(normalizeSupportedTimezone('Nowhere/Unknown')).toBeNull()
    expect(localeDirection('en')).toBe('ltr')
    expect(localeDirection('pt-AO')).toBe('ltr')
  })
})
