import { resolveWidgetLocalization } from '../../src/localization'
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

  it('preserves the legacy greeting when no localizations exist', () => {
    const { localizations: _localizations, ...legacyRule } = rule
    const result = resolveWidgetLocalization(legacyRule as WidgetRule, 'en', ['en-US'])
    expect(result.content.prompt).toBe('Legacy prompt')
    expect(result.content.submit_label).toBe('Send feedback')
  })
})
