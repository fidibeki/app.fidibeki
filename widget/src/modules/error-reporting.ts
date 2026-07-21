import * as Sentry from '@sentry/browser'
import type { ErrorEvent as SentryErrorEvent } from '@sentry/browser'

let initializedDSN = ''

export function captureWidgetError(
  error: unknown,
  options: { dsn: string; context: Record<string, unknown> },
): void {
  if (!options.dsn) return
  if (initializedDSN !== options.dsn) {
    Sentry.init({
      dsn: options.dsn,
      beforeSend(event) {
        return scrubEvent(event)
      },
    })
    initializedDSN = options.dsn
  }
  Sentry.captureException(error, { extra: scrubRecord(options.context) })
}

export function scrubEvent(event: SentryErrorEvent): SentryErrorEvent {
  const scrubbed = structuredClone(event)
  delete scrubbed.user
  if (isRecord(scrubbed.request)) {
    delete scrubbed.request.data
    delete scrubbed.request.cookies
    delete scrubbed.request.headers
    if (typeof scrubbed.request.url === 'string') {
      scrubbed.request.url = scrubbed.request.url.split('?')[0] ?? ''
    }
  }
  if (Array.isArray(scrubbed.breadcrumbs)) {
    scrubbed.breadcrumbs = scrubbed.breadcrumbs.map((breadcrumb) => isRecord(breadcrumb) ? scrubRecord(breadcrumb) : breadcrumb)
  }
  if (scrubbed.extra) scrubbed.extra = scrubRecord(scrubbed.extra)
  return scrubbed
}

function scrubRecord(record: Record<string, unknown>): Record<string, unknown> {
  const sensitive = /email|token|authorization|cookie|text|html|value/i
  return Object.fromEntries(Object.entries(record).map(([key, value]) => {
    if (sensitive.test(key)) return [key, '[Filtered]']
    if (isRecord(value)) return [key, scrubRecord(value)]
    return [key, value]
  }))
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
