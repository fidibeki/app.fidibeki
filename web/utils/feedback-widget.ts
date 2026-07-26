/**
 * We run our own widget on the backoffice, the same way a customer runs it on
 * their site: one async script tag carrying a public project key.
 *
 * The two values come from configuration rather than the browser's origin. The
 * assets are served from a different host than the app, so a guess would produce
 * a script tag that resolves to nothing — the same reason widgetUrl exists for
 * the embed snippets we hand out.
 */
export function loaderScriptURL(widgetUrl: string, key: string): string {
  if (!widgetUrl.trim() || !key.trim()) return ''
  return `${widgetUrl.trim().replace(/\/+$/, '')}/v1/loader.js`
}

/**
 * Adds the loader once, and reports whether it did.
 *
 * Idempotent on the key attribute rather than on a flag of our own: the script
 * survives client-side navigation, so the second call has to find the first
 * one's tag rather than trust a variable that a page reload resets.
 */
export function mountFeedbackWidget(document: Document, widgetUrl: string, key: string): boolean {
  const source = loaderScriptURL(widgetUrl, key)
  if (!source) return false
  if (document.querySelector('script[data-fidibeki-key]')) return false
  const script = document.createElement('script')
  script.async = true
  script.src = source
  script.dataset.fidibekiKey = key.trim()
  document.head.append(script)
  return true
}
