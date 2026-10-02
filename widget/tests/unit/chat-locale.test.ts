import { describe, expect, it } from 'vitest'
import { visitorLocale } from '../../src/chat-locale'

describe('visitor browser language', () => {
  it('keeps the preferred language and region rather than the page language', () => {
    expect(visitorLocale(['pt-AO', 'en-US'], 'en-US')).toBe('pt-AO')
    expect(visitorLocale(['zh-Hant-TW'], 'en')).toBe('zh-Hant-TW')
    expect(visitorLocale(['es-419'], 'en')).toBe('es-419')
  })
  it('strips private extensions and falls back safely', () => {
    expect(visitorLocale(['en-US-u-ca-gregory-x-private'], '')).toBe('en-US')
    expect(visitorLocale(['invalid_value', 'fr-FR'], 'en')).toBe('fr-FR')
    expect(visitorLocale([], 'pt-PT')).toBe('pt-PT')
    expect(visitorLocale(['<script>'], '')).toBe('')
  })
})
