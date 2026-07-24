import * as Sentry from '@sentry/browser'
import type { ErrorEvent as SentryErrorEvent, Exception } from '@sentry/browser'

type Mechanism = NonNullable<Exception['mechanism']>

let initializedDSN = ''

const ALLOWED_CONTEXT_KEYS = ['phase', 'project_id', 'widget_rule_id'] as const

// Every free-text value ScrubEvent (backend) / scrubEvent (here) cannot prove is free of customer
// data is replaced with this fixed placeholder: a thrown error's message/stack text is unstructured
// and may embed reporter/page content, so the type and stack trace — not the literal text — carry
// the evidence that survives. Kept identical to backend/internal/observability/scrub.go's constant
// so a Sentry issue reads the same way from either SDK.
export const REDACTED_TELEMETRY_VALUE =
  '[fidibeki] value removed before Sentry send; see exception type and stacktrace'

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
  Sentry.captureException(error, { extra: options.context })
}

// scrubEvent reconstructs the outgoing event from an explicit allowlist of known-safe fields
// instead of deleting known-bad ones from the original, so a field this function does not name —
// whether a caller adds a new context key or a future @sentry/browser upgrade adds a new event
// field — is absent from the scrubbed event by construction rather than by remembering to extend a
// blocklist.
//
// Kept as-is: event_id, timestamp, platform, level, release, environment, sdk, type,
// exception.values[].type/stacktrace/mechanism.type/mechanism.handled.
//
// Genericized: message and exception.values[].value (replaced with REDACTED_TELEMETRY_VALUE when
// set), since a thrown string/Error's text may embed page or reporter content.
//
// Always dropped: request (no URL at all — paths may contain personal data), user, contexts, tags,
// breadcrumbs (all), exception.values[].mechanism.data, and every extra key except the widget's own
// bounded operational context (phase/project_id/widget_rule_id, string values only).
export function scrubEvent(event: SentryErrorEvent): SentryErrorEvent {
  const context = allowedContext(event.extra)
  return {
    type: event.type,
    ...(event.event_id !== undefined && { event_id: event.event_id }),
    ...(event.timestamp !== undefined && { timestamp: event.timestamp }),
    ...(event.platform !== undefined && { platform: event.platform }),
    ...(event.level !== undefined && { level: event.level }),
    ...(event.release !== undefined && { release: event.release }),
    ...(event.environment !== undefined && { environment: event.environment }),
    ...(event.sdk !== undefined && { sdk: event.sdk }),
    ...(event.message !== undefined && { message: REDACTED_TELEMETRY_VALUE }),
    ...(event.exception !== undefined && {
      exception: { values: (event.exception.values ?? []).map(scrubException) },
    }),
    ...(context !== undefined && { extra: context }),
  }
}

function scrubException(exception: Exception): Exception {
  return {
    ...(exception.type !== undefined && { type: exception.type }),
    ...(exception.stacktrace !== undefined && { stacktrace: exception.stacktrace }),
    ...(exception.value !== undefined && { value: REDACTED_TELEMETRY_VALUE }),
    ...(exception.mechanism !== undefined && { mechanism: scrubMechanism(exception.mechanism) }),
  }
}

function scrubMechanism(mechanism: Mechanism): Mechanism {
  return {
    type: mechanism.type,
    ...(mechanism.handled !== undefined && { handled: mechanism.handled }),
  }
}

function allowedContext(extra: SentryErrorEvent['extra']): Record<string, unknown> | undefined {
  if (!isRecord(extra)) return undefined
  const result: Record<string, unknown> = {}
  for (const key of ALLOWED_CONTEXT_KEYS) {
    if (typeof extra[key] === 'string') result[key] = extra[key]
  }
  return Object.keys(result).length > 0 ? result : undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
