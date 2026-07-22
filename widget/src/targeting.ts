export interface CanonicalPage {
  target: string
  pathname: string
}

export function canonicalPage(rawURL: string): CanonicalPage | null {
  try {
    const url = new URL(rawURL)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    const pathname = url.pathname === '/' ? '/' : url.pathname.replace(/\/+$/, '') || '/'
    return { target: `${url.origin}${pathname}`, pathname }
  } catch {
    return null
  }
}
