export const supportedLocales = ['en', 'pt', 'pt-AO', 'pt-PT', 'en-XA'] as const

export type SupportedLocale = typeof supportedLocales[number]
export type RegionalPreferenceSource = 'member' | 'organization' | 'browser' | 'default'
export type TextDirection = 'ltr' | 'rtl'

export interface RegionalPreferenceInput {
  memberLocale?: string | null
  memberTimezone?: string | null
  tenantLocale?: string | null
  tenantTimezone?: string | null
  browserLocales?: readonly string[]
  browserTimezone?: string | null
}

export interface ResolvedRegionalPreferences {
  locale: SupportedLocale
  localeSource: RegionalPreferenceSource
  timezone: string
  timezoneSource: RegionalPreferenceSource
}

const normalizedLocales = new Map<string, SupportedLocale>(
  supportedLocales.map(locale => [locale.toLowerCase(), locale]),
)

export function normalizeSupportedLocale(value?: string | null): SupportedLocale | null {
  const normalized = value?.trim().replaceAll('_', '-').toLowerCase()
  return normalized ? normalizedLocales.get(normalized) || null : null
}

export function matchBrowserLocale(values: readonly string[]): SupportedLocale | null {
  for (const value of values) {
    const exact = normalizeSupportedLocale(value)
    if (exact) return exact
    const language = value.trim().split(/[-_]/, 1)[0]?.toLowerCase()
    if (language === 'pt') return 'pt'
    if (language === 'en') return 'en'
  }
  return null
}

export function parseAcceptLanguage(header?: string | null): string[] {
  if (!header) return []
  return header
    .split(',')
    .map((entry, position) => {
      const [rawLocale, ...parameters] = entry.trim().split(';')
      const locale = rawLocale?.trim() || ''
      const qualityParameter = parameters
        .map(parameter => parameter.trim())
        .find(parameter => parameter.toLowerCase().startsWith('q='))
      const quality = qualityParameter
        ? Number.parseFloat(qualityParameter.slice(2))
        : 1
      return { locale, position, quality }
    })
    .filter(entry =>
      entry.locale !== '*' &&
      entry.locale !== '' &&
      Number.isFinite(entry.quality) &&
      entry.quality > 0,
    )
    .sort((left, right) => right.quality - left.quality || left.position - right.position)
    .map(entry => entry.locale)
}

export function normalizeSupportedTimezone(value?: string | null): string | null {
  const timezone = value?.trim()
  if (!timezone || timezone === 'Local' || timezone.length > 100) return null
  try {
    return new Intl.DateTimeFormat('en', { timeZone: timezone }).resolvedOptions().timeZone
  } catch {
    return null
  }
}

export function localeDirection(_locale: SupportedLocale): TextDirection {
  return 'ltr'
}

export function resolveRegionalPreferences(
  input: RegionalPreferenceInput,
): ResolvedRegionalPreferences {
  const memberLocale = normalizeSupportedLocale(input.memberLocale)
  const tenantLocale = normalizeSupportedLocale(input.tenantLocale)
  const browserLocale = matchBrowserLocale(input.browserLocales || [])
  const locale = memberLocale || tenantLocale || browserLocale || 'en'
  const localeSource: RegionalPreferenceSource = memberLocale
    ? 'member'
    : tenantLocale
      ? 'organization'
      : browserLocale
        ? 'browser'
        : 'default'

  const memberTimezone = normalizeSupportedTimezone(input.memberTimezone)
  const tenantTimezone = normalizeSupportedTimezone(input.tenantTimezone)
  const browserTimezone = normalizeSupportedTimezone(input.browserTimezone)
  const timezone = memberTimezone || tenantTimezone || browserTimezone || 'UTC'
  const timezoneSource: RegionalPreferenceSource = memberTimezone
    ? 'member'
    : tenantTimezone
      ? 'organization'
      : browserTimezone
        ? 'browser'
        : 'default'

  return { locale, localeSource, timezone, timezoneSource }
}
