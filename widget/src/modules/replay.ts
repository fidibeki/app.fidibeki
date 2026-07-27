import { record } from 'rrweb'
import { privacySafeLocation } from '../privacy-location'
import type { WidgetRule } from '../types'
import {
  createReplayMaskTextFn,
  isUnserializedReplayEvent,
  replayMaskTextSelector,
  sanitizeReplayEvent,
} from './privacy'

const batchEventLimit = 50
const batchTargetBytes = 200 * 1024
const batchFlushMilliseconds = 15 * 1000
const idleTimeoutMilliseconds = 5 * 60 * 1000
const maximumDurationSeconds = 30 * 60
const maximumEvents = 50_000
const retryDelays = [0, 250, 1000]

export interface ReplayCapture {
  readonly sessionID: string
  stop(): Promise<void>
  revoke(): void
}

export interface ReplayCaptureOptions {
  rule: WidgetRule
  publicKey: string
  apiBase: URL
  consentToken: string
  visitorID?: string
  onError?: (error: unknown) => void
  random?: () => number
}

interface SessionResponse {
  id: string
  rrweb_version: string
  max_duration_seconds: number
  max_events: number
}

interface BatchResponse {
  presigned_url: string
  method: 'PUT'
  headers: Record<string, string>
  complete_token: string
}

interface PendingBatch {
  index: number
  body: Blob
  eventsCount: number
}

export async function startReplayCapture(options: ReplayCaptureOptions): Promise<ReplayCapture | null> {
  const sampled = options.rule.replay_mode === 'full' ||
    (options.rule.replay_mode === 'sampling' &&
      (options.random?.() ?? Math.random()) * 100 < options.rule.replay_sampling_rate)
  if (!sampled) return null

  const clientSessionID = randomID()
  const location = privacySafeLocation(window.location.href, options.rule)
  if (!location) throw new Error('replay session page URL could not be projected safely')
  const session = await createSession(options, clientSessionID, location.target)
  const uploadAbortController = new AbortController()
  let stopped = false
  let revoked = false
  let stopPromise: Promise<void> | null = null
  let eventCount = 0
  let batchIndex = 0
  let completedBatches = 0
  let serializedEvents: string[] = []
  let serializedBytes = 2
  let uploadChain = Promise.resolve()
  let flushTimer = 0
  let idleTimer = 0
  let durationTimer = 0

  const maxEvents = Math.min(maximumEvents, session.max_events, options.rule.replay_max_events)
  const maxDuration = Math.min(
    maximumDurationSeconds,
    session.max_duration_seconds,
    options.rule.replay_max_duration_seconds,
  )
  const enqueueBatch = () => {
    if (serializedEvents.length === 0) return
    const batch: PendingBatch = {
      index: batchIndex++,
      body: new Blob([`[${serializedEvents.join(',')}]`], { type: 'application/json' }),
      eventsCount: serializedEvents.length,
    }
    serializedEvents = []
    serializedBytes = 2
    uploadChain = uploadChain.then(async () => {
      if (revoked) return
      await uploadBatch(options, session.id, batch, uploadAbortController.signal)
      completedBatches++
    })
  }

  const captureEvent = (event: unknown) => {
    if (stopped || eventCount >= maxEvents) return
    // Interactions inside blocked regions arrive with mirror id -1 and cannot replay;
    // dropping them is expected policy, not a sanitization failure worth reporting.
    if (isUnserializedReplayEvent(event)) return
    const sanitized = sanitizeReplayEvent(
      event,
      location.target,
      id => record.mirror.getNode(id),
    )
    if (sanitized === null) {
      options.onError?.(new Error('replay event failed anonymity sanitization and was dropped'))
      return
    }
    const serialized = JSON.stringify(sanitized)
    const eventBytes = new TextEncoder().encode(serialized).byteLength + (serializedEvents.length > 0 ? 1 : 0)
    if (serializedEvents.length > 0 && serializedBytes+eventBytes > batchTargetBytes) enqueueBatch()
    serializedEvents.push(serialized)
    serializedBytes += eventBytes
    eventCount++
    if (serializedEvents.length >= batchEventLimit || serializedBytes >= batchTargetBytes) enqueueBatch()
    if (eventCount >= maxEvents) queueMicrotask(() => void stop())
  }

  const stopRecording = record<unknown>({
    emit: captureEvent,
    // Input and text masking are anonymity-safe defaults, not customer-configurable: they do
    // not depend on `replay_mask_all_inputs`/`replay_mask_text_selector` so a misconfigured or
    // absent rule setting can never weaken the default. See modules/privacy.ts.
    maskAllInputs: true,
    maskTextSelector: replayMaskTextSelector(),
    maskTextFn: createReplayMaskTextFn(),
    blockSelector: '[data-fidibeki-block]',
    slimDOMOptions: true,
    // Anonymous replay cannot prove that rendered pixels contain no identity.
    recordCanvas: false,
    inlineImages: false,
    collectFonts: false,
    errorHandler: (error) => options.onError?.(error),
  })

  const resetIdleTimer = () => {
    window.clearTimeout(idleTimer)
    idleTimer = window.setTimeout(() => void stop(), idleTimeoutMilliseconds)
  }
  const onNavigation = () => {
    record.addCustomEvent('navigation', { url: location.target })
  }
  const onPageHide = () => void stop()
  const activityEvents: Array<keyof WindowEventMap> = ['pointerdown', 'keydown', 'scroll', 'touchstart']
  for (const event of activityEvents) window.addEventListener(event, resetIdleTimer, { passive: true })
  window.addEventListener('fidibeki:navigation', onNavigation)
  window.addEventListener('pagehide', onPageHide, { once: true })
  flushTimer = window.setInterval(enqueueBatch, batchFlushMilliseconds)
  resetIdleTimer()
  durationTimer = window.setTimeout(() => void stop(), Math.max(1, maxDuration) * 1000)

  const cleanup = () => {
    stopRecording?.()
    window.clearInterval(flushTimer)
    window.clearTimeout(idleTimer)
    window.clearTimeout(durationTimer)
    for (const event of activityEvents) window.removeEventListener(event, resetIdleTimer)
    window.removeEventListener('fidibeki:navigation', onNavigation)
    window.removeEventListener('pagehide', onPageHide)
  }

  function stop(): Promise<void> {
    if (stopPromise) return stopPromise
    stopped = true
    cleanup()
    enqueueBatch()
    stopPromise = uploadChain
      .then(async () => {
        if (completedBatches > 0) await finalizeSession(options, session.id)
      })
      .catch((error: unknown) => {
        options.onError?.(error)
      })
    return stopPromise
  }

  function revoke(): void {
    if (stopped) return
    stopped = true
    revoked = true
    uploadAbortController.abort()
    cleanup()
    serializedEvents = []
    serializedBytes = 2
    void uploadChain.catch(() => {})
  }

  return { sessionID: session.id, stop, revoke }
}

async function createSession(
  options: ReplayCaptureOptions,
  sessionID: string,
  pageURL: string,
): Promise<SessionResponse> {
  const response = await widgetFetch(options, 'replay/sessions', {
    method: 'POST',
    body: JSON.stringify({
      session_id: sessionID,
      widget_rule_id: options.rule.id,
      page_url: pageURL,
      visitor_id: options.visitorID,
      consent_token: options.consentToken,
    }),
  })
  if (!response.ok) throw new Error(`replay session creation failed with status ${response.status}`)
  const session = await response.json() as SessionResponse
  if (!session.id || session.rrweb_version !== '2.1.1') {
    throw new Error('replay session response is incompatible')
  }
  return session
}

async function uploadBatch(
  options: ReplayCaptureOptions,
  sessionID: string,
  batch: PendingBatch,
  signal: AbortSignal,
): Promise<void> {
  await retry(async () => {
    const declared = await widgetFetch(options, `replay/sessions/${sessionID}/batches`, {
      method: 'POST',
      signal,
      body: JSON.stringify({
        batch_index: batch.index,
        events_count: batch.eventsCount,
        size_bytes: batch.body.size,
      }),
    })
    if (!declared.ok) throw new Error(`replay batch declaration failed with status ${declared.status}`)
    const upload = await declared.json() as BatchResponse
    const stored = await fetch(upload.presigned_url, {
      method: upload.method,
      headers: upload.headers,
      body: batch.body,
      signal,
      credentials: 'omit',
    })
    if (!stored.ok) throw new Error(`replay batch upload failed with status ${stored.status}`)
    const completed = await widgetFetch(
      options,
      `replay/sessions/${sessionID}/batches/${batch.index}/complete`,
      { method: 'POST', signal, body: JSON.stringify({ complete_token: upload.complete_token }) },
    )
		// A lost 204 response makes the retry observe an already-completed batch.
		if (!completed.ok && completed.status !== 409) {
			throw new Error(`replay batch completion failed with status ${completed.status}`)
		}
  }, signal)
}

async function finalizeSession(options: ReplayCaptureOptions, sessionID: string): Promise<void> {
  await retry(async () => {
    const response = await widgetFetch(options, `replay/sessions/${sessionID}/finalize`, {
      method: 'POST', body: '{}', keepalive: true,
    })
    if (!response.ok) throw new Error(`replay finalization failed with status ${response.status}`)
  })
}

function widgetFetch(options: ReplayCaptureOptions, path: string, init: RequestInit): Promise<Response> {
  const url = new URL(path, options.apiBase)
  url.searchParams.set('key', options.publicKey)
  return fetch(url, {
    ...init,
    mode: 'cors',
    credentials: 'omit',
    headers: {
      'Content-Type': 'application/json',
      'X-Fidibeki-Public-Key': options.publicKey,
      ...init.headers,
    },
  })
}

async function retry(operation: () => Promise<void>, signal?: AbortSignal): Promise<void> {
  let lastError: unknown
  for (const delay of retryDelays) {
    throwIfAborted(signal)
    if (delay > 0) await new Promise((resolve) => window.setTimeout(resolve, delay))
    throwIfAborted(signal)
    try {
      await operation()
      return
    } catch (error) {
      throwIfAborted(signal)
      lastError = error
    }
  }
  throw lastError
}

function throwIfAborted(signal?: AbortSignal): void {
  if (!signal?.aborted) return
  throw signal.reason ?? new DOMException('The operation was aborted', 'AbortError')
}

function randomID(): string {
  const cryptoObject = globalThis.crypto as Crypto & { randomUUID?: () => string }
  if (typeof cryptoObject.randomUUID === 'function') return cryptoObject.randomUUID()
  const bytes = cryptoObject.getRandomValues(new Uint8Array(16))
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80
  const hex = Array.from(bytes).map((byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
