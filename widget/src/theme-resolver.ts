import type { WidgetTheme } from './types'

export type ResolvedTheme = 'light' | 'dark'

export interface ResolveThemeOptions {
  // Priority 1: the `theme` attribute on the custom element (e.g.
  // `<fidibeki-widget theme="dark">` or `<fidibeki-wall theme="light">).
  // `null` when the widget is mounted via a script tag with no custom element.
  elementAttr?: string | null
  // Priority 2: the host page's active colour. Read from `data-theme` on the
  // closest ancestor (including `document.documentElement`), so a site that
  // drives its own dark mode via `<html data-theme="dark">` is honoured.
  hostDataTheme?: string | null
  // Priority 3: the server-published theme. `auto` means "let the system
  // decide", so an explicit `light`/`dark` here wins over the OS only when
  // priorities 1 and 2 are absent.
  fallback?: WidgetTheme
  // The element whose ancestry is walked for `data-theme`. Defaults to
  // `document.documentElement` so the common `<html data-theme="...">` case
  // works without a host element.
  host?: HTMLElement | null
}

/**
 * Resolves a concrete `light` | `dark` theme from three optional sources, in
 * priority order:
 *
 * 1. `elementAttr` — the `theme` attribute on the custom element
 * 2. `hostDataTheme` — the closest ancestor's `data-theme` (or `<html>`)
 * 3. `fallback` — the server-published theme; `auto` delegates to the OS via
 *    `prefers-color-scheme`
 *
 * The result is always a concrete value so downstream CSS is deterministic and
 * never re-runs a `@media` query at runtime.
 */
export function resolveTheme(options: ResolveThemeOptions): ResolvedTheme {
  const fromAttribute = normalizeTheme(options.elementAttr)
  if (fromAttribute) return fromAttribute

  const fromHost = options.hostDataTheme ?? readHostDataTheme(options.host)
  const normalizedHost = normalizeTheme(fromHost)
  if (normalizedHost) return normalizedHost

  const fallback = options.fallback ?? 'auto'
  if (fallback === 'light' || fallback === 'dark') return fallback

  return systemTheme()
}

/** Sets `theme="light"` | `theme="dark"` on `host` so existing CSS selectors
 *  like `:host([theme="dark"])` keep working without a rewrite. */
export function applyHostTheme(host: HTMLElement, resolved: ResolvedTheme): void {
  host.setAttribute('theme', resolved)
}

/** Reads `data-theme` from the closest ancestor carrying it, falling back to
 *  `document.documentElement` so `<html data-theme="...">` is the baseline. */
export function readHostDataTheme(host?: HTMLElement | null): string | null {
  const root = host?.ownerDocument ?? document
  const carrier =
    host?.closest?.('[data-theme]') ?? root.documentElement
  return carrier?.getAttribute('data-theme') ?? null
}

/** Returns the OS theme via `prefers-color-scheme`. Defaults to `light` when
 *  `matchMedia` is unavailable (SSR, older browsers, test envs). */
export function systemTheme(): ResolvedTheme {
  const query = mediaQuery()
  if (!query) return 'light'
  return query.matches ? 'dark' : 'light'
}

/** Subscribes to OS theme changes. Returns an unsubscribe function, or `null`
 *  when `matchMedia` is unavailable. */
export function subscribeToSystemTheme(listener: () => void): () => void {
  const query = mediaQuery()
  if (!query) return () => {}
  const handler = () => listener()
  query.addEventListener('change', handler)
  return () => query.removeEventListener('change', handler)
}

/** Subscribes to `data-theme` changes on `target` (default
 *  `document.documentElement`). Returns an unsubscribe function. */
export function subscribeToHostDataTheme(
  listener: () => void,
  target: HTMLElement = document.documentElement,
): () => void {
  const observer = new MutationObserver(listener)
  observer.observe(target, {
    attributes: true,
    attributeFilter: ['data-theme'],
  })
  return () => observer.disconnect()
}

function normalizeTheme(value: string | null | undefined): ResolvedTheme | null {
  if (value === 'light' || value === 'dark') return value
  return null
}

function mediaQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function')
    return null
  return window.matchMedia('(prefers-color-scheme: dark)')
}