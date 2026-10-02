export const marketingLocales = [
  { code: 'en', language: 'en', name: 'English', file: 'en.json' },
  { code: 'pt', language: 'pt-PT', name: 'Português', file: 'pt.json' },
  { code: 'es', language: 'es', name: 'Español', file: 'es.json' },
  { code: 'fr', language: 'fr', name: 'Français', file: 'fr.json' },
] as const
export type MarketingLocale = (typeof marketingLocales)[number]['code']

/** Stable catalog keys allow shared English copy to have one translation. */
export function messageKey(source: string) {
  let hash = 2166136261
  for (const character of source.replace(/\s+/g, ' ').trim()) {
    hash ^= character.charCodeAt(0)
    hash = Math.imul(hash, 16777619)
  }
  return `m${(hash >>> 0).toString(16)}`
}

export function unlocalizedPath(path: string) {
  return path.replace(/^\/(?:pt|es|fr)(?=\/|[?#]|$)/, '') || '/'
}

export function localizedPath(path: string, locale: string) {
  if (!path.startsWith('/') || path.startsWith('//')) return path
  const base = unlocalizedPath(path)
  const rooted = base.startsWith('/') ? base : `/${base}`
  return locale === 'en'
    ? rooted
    : `/${locale}${rooted === '/' ? '' : rooted.replace(/^\/(?=[?#])/, '')}`
}

export function markdownPath(path: string) {
  return path === '/' || /^\/(?:pt|es|fr)$/.test(path)
    ? `${path === '/' ? '' : path}/index.md`
    : `${path}.md`
}
