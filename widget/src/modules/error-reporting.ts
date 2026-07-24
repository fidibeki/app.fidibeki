import * as Sentry from '@sentry/browser'
import type { ErrorEvent as SentryErrorEvent, Exception } from '@sentry/browser'

type Mechanism = NonNullable<Exception['mechanism']>
type Stacktrace = NonNullable<Exception['stacktrace']>
type StackFrame = NonNullable<Stacktrace['frames']>[number]

let initializedDSN = ''

const ALLOWED_PHASES: ReadonlySet<string> = new Set([
  'analytics',
  'targeting',
  'mount',
  'configuration',
  'replay',
  'replay_consent_prompt',
  'analytics_consent_prompt',
  'attachments',
  'submission',
])
const OPERATIONAL_IDENTIFIER = /^[A-Za-z0-9_-]{1,128}$/

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
// Kept as-is: event_id, timestamp, platform, level, release, environment, type,
// exception.values[].type, structural stack-frame fields, and mechanism.type/handled.
//
// Genericized: message and exception.values[].value (replaced with REDACTED_TELEMETRY_VALUE when
// set), since a thrown string/Error's text may embed page or reporter content.
//
// Always dropped: request (no URL at all — paths may contain personal data), user, contexts, tags,
// breadcrumbs (all), sdk metadata, stack-frame absolute paths/source context/locals,
// exception.values[].mechanism.data, and every extra key except the widget's own bounded
// operational context (known phase plus safe project/rule identifiers).
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
    ...(exception.stacktrace !== undefined && { stacktrace: scrubStacktrace(exception.stacktrace) }),
    ...(exception.value !== undefined && { value: REDACTED_TELEMETRY_VALUE }),
    ...(exception.mechanism !== undefined && { mechanism: scrubMechanism(exception.mechanism) }),
  }
}

function scrubStacktrace(stacktrace: Stacktrace): Stacktrace {
  return {
    ...(stacktrace.frames !== undefined && { frames: stacktrace.frames.map(scrubStackFrame) }),
    ...(stacktrace.frames_omitted !== undefined && {
      frames_omitted: [...stacktrace.frames_omitted] as [number, number],
    }),
  }
}

function scrubStackFrame(frame: StackFrame): StackFrame {
  return {
    ...(frame.filename !== undefined && { filename: frame.filename }),
    ...(frame.function !== undefined && { function: frame.function }),
    ...(frame.module !== undefined && { module: frame.module }),
    ...(frame.platform !== undefined && { platform: frame.platform }),
    ...(frame.lineno !== undefined && { lineno: frame.lineno }),
    ...(frame.colno !== undefined && { colno: frame.colno }),
    ...(frame.in_app !== undefined && { in_app: frame.in_app }),
    ...(frame.instruction_addr !== undefined && { instruction_addr: frame.instruction_addr }),
    ...(frame.addr_mode !== undefined && { addr_mode: frame.addr_mode }),
    ...(frame.debug_id !== undefined && { debug_id: frame.debug_id }),
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
  const result: Record<string, string> = {}
  if (typeof extra.phase === 'string' && ALLOWED_PHASES.has(extra.phase)) {
    result.phase = extra.phase
  }
  if (isOperationalIdentifier(extra.project_id)) result.project_id = extra.project_id
  if (isOperationalIdentifier(extra.widget_rule_id)) result.widget_rule_id = extra.widget_rule_id
  return Object.keys(result).length > 0 ? result : undefined
}

function isOperationalIdentifier(value: unknown): value is string {
  return typeof value === 'string' && OPERATIONAL_IDENTIFIER.test(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
