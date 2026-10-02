/** Browser preference, independent of the host page or widget display language. */
export function visitorLocale(languages: readonly string[] = navigator.languages, fallback = navigator.language): string {
  for (const value of [...languages, fallback]) {
    try {
      const locale = new Intl.Locale(value)
      const code = [locale.language, locale.script, locale.region].filter(Boolean).join('-')
      if (/^[a-z]{2,3}(-[A-Z][a-z]{3})?(-([A-Z]{2}|[0-9]{3}))?$/.test(code)) return code
    } catch { /* A malformed preference must not prevent chat from loading. */ }
  }
  return ''
}
