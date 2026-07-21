export interface ResponseCookieHeaders {
  get(name: string): string | null
  getSetCookie?: () => string[]
}

export function forwardResponseCookies(
  headers: ResponseCookieHeaders,
  append: (cookie: string) => void,
) {
  const values = headers.getSetCookie?.() || []
  if (values.length) {
    for (const value of values) append(value)
    return
  }
  const value = headers.get('set-cookie')
  if (value) append(value)
}
