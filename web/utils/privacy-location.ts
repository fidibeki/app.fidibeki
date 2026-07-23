export function specificPageLocation(value?: string | null): string | null {
  if (!value) return null
  try {
    const parsed = new URL(value)
    if (
      (parsed.protocol !== 'http:' && parsed.protocol !== 'https:')
      || !parsed.host
      || parsed.pathname === '/'
    ) return null
    return value
  } catch {
    return null
  }
}
