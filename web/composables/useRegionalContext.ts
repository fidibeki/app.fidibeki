import {
  localeDirection,
  matchBrowserLocale,
  normalizeSupportedTimezone,
  parseAcceptLanguage,
  resolveRegionalPreferences,
  type ResolvedRegionalPreferences,
} from '~/utils/regional'

export const browserLocaleCookieName = 'fidibeki_browser_locale'
export const browserTimezoneCookieName = 'fidibeki_browser_timezone'

const defaultPreferences = (): ResolvedRegionalPreferences => ({
  locale: 'en',
  localeSource: 'default',
  timezone: 'UTC',
  timezoneSource: 'default',
})

export function useRegionalContext() {
  const nuxtApp = useNuxtApp()
  const { member, tenant } = useAuth()
  const preferences = useState<ResolvedRegionalPreferences>(
    'regional:preferences',
    defaultPreferences,
  )
  const cookieOptions = {
    maxAge: 365 * 24 * 60 * 60,
    path: '/',
    sameSite: 'lax' as const,
    secure: import.meta.client && window.location.protocol === 'https:',
  }
  const browserLocaleCookie = useCookie<string | null>(browserLocaleCookieName, cookieOptions)
  const browserTimezoneCookie = useCookie<string | null>(browserTimezoneCookieName, cookieOptions)
  const requestHeaders = import.meta.server
    ? useRequestHeaders(['accept-language'])
    : undefined

  function detectedBrowserLocales(): string[] {
    if (import.meta.server) {
      return parseAcceptLanguage(requestHeaders?.['accept-language'])
    }
    if (nuxtApp.isHydrating) {
      return browserLocaleCookie.value
        ? [browserLocaleCookie.value]
        : [preferences.value.locale]
    }
    return [...(navigator.languages?.length ? navigator.languages : [navigator.language])]
  }

  function detectedBrowserTimezone(): string | null {
    if (import.meta.server || nuxtApp.isHydrating) {
      return browserTimezoneCookie.value || null
    }
    return normalizeSupportedTimezone(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    )
  }

  async function apply(browser?: { locales?: readonly string[], timezone?: string | null }) {
    const resolved = resolveRegionalPreferences({
      memberLocale: member.value?.locale,
      memberTimezone: member.value?.timezone,
      tenantLocale: tenant.value?.default_locale,
      tenantTimezone: tenant.value?.default_timezone,
      browserLocales: browser?.locales || detectedBrowserLocales(),
      browserTimezone: browser?.timezone === undefined
        ? detectedBrowserTimezone()
        : browser.timezone,
    })
    if (nuxtApp.$i18n.locale.value !== resolved.locale) {
      await nuxtApp.$i18n.setLocale(resolved.locale)
    }
    preferences.value = resolved
    return resolved
  }

  async function captureBrowserPreferences() {
    if (import.meta.server) return preferences.value
    const locales = [...(navigator.languages?.length ? navigator.languages : [navigator.language])]
    const locale = matchBrowserLocale(locales)
    const timezone = normalizeSupportedTimezone(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    )
    browserLocaleCookie.value = locale
    browserTimezoneCookie.value = timezone
    return apply({ locales, timezone })
  }

  return {
    preferences: readonly(preferences),
    effectiveLocale: computed(() => preferences.value.locale),
    localeSource: computed(() => preferences.value.localeSource),
    timezone: computed(() => preferences.value.timezone),
    timezoneSource: computed(() => preferences.value.timezoneSource),
    direction: computed(() => localeDirection(preferences.value.locale)),
    apply,
    captureBrowserPreferences,
  }
}
