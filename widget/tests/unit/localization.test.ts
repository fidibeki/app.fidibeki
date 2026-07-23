import {
  resolveWidgetLocalization,
  resolveWidgetLocalizationForPage,
} from '../../src/localization'
import type { WidgetLocalization, WidgetRule } from '../../src/types'

const localized = (prompt: string): WidgetLocalization => ({
  launcher_label: 'Feedback', prompt, comment_placeholder: 'More', contact_label: 'Email',
  consent_label: 'Contact me', submit_label: 'Send', success_message: 'Sent',
})

const rule = {
  greeting: 'Legacy prompt',
  localizations: {
    en: localized('English prompt'),
    pt: localized('Pergunta em português'),
    'pt-AO': localized('Pergunta angolana'),
  },
} as unknown as WidgetRule

describe('widget localization', () => {
  it('prefers the most specific browser locale', () => {
    const result = resolveWidgetLocalization(rule, 'en', ['pt-AO', 'pt'])
    expect(result.locale).toBe('pt-AO')
    expect(result.content.prompt).toBe('Pergunta angolana')
    expect(result.system.sending).toBe('A enviar...')
  })

  it('falls back from a regional locale to its primary language', () => {
    const result = resolveWidgetLocalization(rule, 'en', ['pt-MZ'])
    expect(result.locale).toBe('pt')
    expect(result.content.prompt).toBe('Pergunta em português')
  })

  it('uses the project default before the first configured locale', () => {
    expect(resolveWidgetLocalization(rule, 'en', ['fr-FR']).content.prompt).toBe('English prompt')
    expect(resolveWidgetLocalization(rule, 'de', ['fr-FR']).content.prompt).toBe('English prompt')
  })

  it('uses the declared page locale before browser preferences', () => {
    const result = resolveWidgetLocalizationForPage(rule, 'en', 'pt-AO', ['en-US'])
    expect(result.locale).toBe('pt-AO')
    expect(result.content.prompt).toBe('Pergunta angolana')
  })

  it('falls back from a regional page locale to its primary language', () => {
    const result = resolveWidgetLocalizationForPage(rule, 'en', 'pt-MZ', ['en-US'])
    expect(result.locale).toBe('pt')
    expect(result.content.prompt).toBe('Pergunta em português')
  })

  it('uses the project default when a valid page locale is unsupported', () => {
    const result = resolveWidgetLocalizationForPage(rule, 'en', 'fr-FR', ['pt-AO'])
    expect(result.locale).toBe('en')
    expect(result.content.prompt).toBe('English prompt')
  })

  it('uses browser preferences only when the page locale is absent or invalid', () => {
    expect(resolveWidgetLocalizationForPage(rule, 'en', '', ['pt-AO']).locale).toBe('pt-AO')
    expect(resolveWidgetLocalizationForPage(rule, 'en', 'not_a_locale!', ['pt-MZ']).locale).toBe('pt')
  })

  it('preserves the legacy greeting when no localizations exist', () => {
    const { localizations: _localizations, ...legacyRule } = rule
    const result = resolveWidgetLocalization(legacyRule as WidgetRule, 'en', ['en-US'])
    expect(result.content.prompt).toBe('Legacy prompt')
    expect(result.content.submit_label).toBe('Send feedback')
  })

  it('localizes Fidibeki system copy for legacy rules without replacing customer copy', () => {
    const { localizations: _localizations, ...legacyRule } = rule
    const result = resolveWidgetLocalizationForPage(legacyRule as WidgetRule, 'en', 'pt-AO', ['en-US'])
    expect(result.locale).toBe('pt-ao')
    expect(result.content.prompt).toBe('Legacy prompt')
    expect(result.system.close).toBe('Fechar feedback')
  })

  it('provides the required consent notice and privacy link copy in every supported system locale', () => {
    for (const locale of ['en', 'pt']) {
      const result = resolveWidgetLocalization(rule, 'en', [locale])
      expect(result.system.consentNotice.length).toBeGreaterThan(0)
      expect(result.system.privacyPolicy.length).toBeGreaterThan(0)
    }
    expect(resolveWidgetLocalization(rule, 'en', ['en']).system.consentNotice)
      .not.toBe(resolveWidgetLocalization(rule, 'en', ['pt']).system.consentNotice)
  })

  it('falls back to English consent copy for the legacy default localization', () => {
    const { localizations: _localizations, ...legacyRule } = rule
    const result = resolveWidgetLocalization(legacyRule as WidgetRule, 'en', ['en'])
    expect(result.content.consent_label).toBe('Email me updates about this feedback')
  })
})
