import { describe, expect, it, vi } from 'vitest'
import { adminMessages as adminEnglish } from '../../i18n/messages/admin.en'
import { adminMessages as adminPortuguese } from '../../i18n/messages/admin.pt'
import { feedbackMessages as feedbackEnglish } from '../../i18n/messages/feedback.en'
import { feedbackMessages as feedbackPortuguese } from '../../i18n/messages/feedback.pt'
import {
  catalogParity,
  findVisibleLiterals,
  flattenCatalog,
  interpolationTokens,
  pseudoLocalizeCatalog,
  pseudoLocalizeMessage,
  type MessageCatalog,
} from '../../utils/i18n-quality'

describe('i18n quality controls', () => {
  it('expands visible text while preserving interpolation and plural syntax', () => {
    const source = 'One project for {workspace} | {count} projects for {workspace}'
    const result = pseudoLocalizeMessage(source)

    expect(result).toContain('{workspace}')
    expect(result).toContain('{count}')
    expect(result.split('|')).toHaveLength(2)
    expect(result.length).toBeGreaterThan(source.length * 1.25)
    expect(interpolationTokens(result)).toEqual(interpolationTokens(source))
  })

  it('pseudo-localizes every catalog leaf without mutating the source', () => {
    const source: MessageCatalog = {
      navigation: { overview: 'Overview' },
      counter: 'One item | {count} items',
    }

    const result = pseudoLocalizeCatalog(source)

    expect(flattenCatalog(result).keys().toArray()).toEqual(
      flattenCatalog(source).keys().toArray(),
    )
    expect(result).not.toBe(source)
    expect(source.navigation).toEqual({ overview: 'Overview' })
    expect(result.navigation).toEqual({ overview: '[ØØvëërvïïëëw ~~~]' })
  })

  it('reports missing, extra, and interpolation-incompatible messages', () => {
    const result = catalogParity(
      {
        welcome: 'Welcome {name}',
        nested: { saved: 'Saved' },
      },
      {
        welcome: 'Bem-vindo {member}',
        unexpected: 'Unexpected',
      },
    )

    expect(result).toEqual({
      missing: ['nested.saved'],
      extra: ['unexpected'],
      interpolationMismatches: [{
        key: 'welcome',
        referenceTokens: ['{name}'],
        candidateTokens: ['{member}'],
      }],
    })
  })

  it.each([
    ['administration', adminEnglish, adminPortuguese],
    ['feedback', feedbackEnglish, feedbackPortuguese],
  ])('keeps the %s English and Portuguese catalogs compatible', (_name, english, portuguese) => {
    expect(catalogParity(english, portuguese)).toEqual({
      missing: [],
      extra: [],
      interpolationMismatches: [],
    })
  })

  it('keeps complete catalogs compatible except for the intentional fallback probe', async () => {
    vi.stubGlobal('defineI18nLocale', (factory: () => MessageCatalog) => factory)
    try {
      const [{ default: englishFactory }, { default: portugueseFactory }] = await Promise.all([
        import('../../i18n/locales/en'),
        import('../../i18n/locales/pt'),
      ])
      const english = (englishFactory as unknown as () => MessageCatalog)()
      const portuguese = (portugueseFactory as unknown as () => MessageCatalog)()
      const parity = catalogParity(english, portuguese)

      expect(parity).toEqual({
        missing: ['runtime.fallbackProbe'],
        extra: [],
        interpolationMismatches: [],
      })
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('finds visible template text and literal accessibility attributes', () => {
    const source = `<script setup lang="ts">
const label = 'Customer-authored value'
useHead({ title: 'Static page title' })
const notice = ref('')
notice.value = 'Saved successfully.'
</script>
<template>
  <!-- Documentation text is not rendered. -->
  <section aria-label="Feedback queue" :title="label">
    <h1>Review feedback</h1>
    <p>{{ count < 2 ? label : otherLabel }}</p>
    <input placeholder="Search records" :aria-label="label">
    <output :aria-label="plural('items.count', count)">{{ count }}</output>
    <button :title="ready ? 'Archive issue' : 'Restore issue'">...</button>
    <button :aria-label="ready ? t('actions.ready') : 'Raw fallback'">...</button>
    <span>{{ ready ? label : 'No result available' }}</span>
  </section>
</template>`

    expect(findVisibleLiterals(source)).toEqual([
      { kind: 'script', line: 3, value: 'Static page title' },
      { kind: 'script', line: 5, value: 'Saved successfully.' },
      { kind: 'attribute', line: 9, value: 'Feedback queue' },
      { kind: 'text', line: 10, value: 'Review feedback' },
      { kind: 'attribute', line: 12, value: 'Search records' },
      { kind: 'attribute', line: 14, value: "ready ? 'Archive issue' : 'Restore issue'" },
      { kind: 'attribute', line: 15, value: "ready ? t('actions.ready') : 'Raw fallback'" },
      { kind: 'expression', line: 16, value: 'No result available' },
    ])
  })
})
