export interface CanonicalPage {
  target: string
  pathname: string
}

export function canonicalPage(rawURL: string): CanonicalPage | null {
  try {
    const url = new URL(rawURL)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    const collapsed = url.pathname.replace(/\/{2,}/g, '/')
    const pathname = collapsed === '/' ? '/' : collapsed.replace(/\/+$/, '') || '/'
    return { target: `${url.origin}${pathname}`, pathname }
  } catch {
    return null
  }
}
