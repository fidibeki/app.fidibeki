import { record } from 'rrweb'
import type { WidgetRule } from '../types'

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
}

export interface ReplayCaptureOptions {
  rule: WidgetRule
  publicKey: string
  apiBase: URL
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
  const session = await createSession(options, clientSessionID)
  let stopped = false
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
  const recordCanvas = options.rule.replay_record_canvas

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
      await uploadBatch(options, session.id, batch)
      completedBatches++
    })
  }

  const captureEvent = (event: unknown) => {
    if (stopped || eventCount >= maxEvents) return
    const serialized = JSON.stringify(event)
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
    maskAllInputs: options.rule.replay_mask_all_inputs,
		...(options.rule.replay_mask_text_selector
			? { maskTextSelector: options.rule.replay_mask_text_selector }
			: {}),
    blockSelector: '[data-fidibeki-block]',
    slimDOMOptions: true,
    recordCanvas,
    ...(recordCanvas ? { sampling: { canvas: options.rule.replay_canvas_fps } } : {}),
    inlineImages: false,
    collectFonts: false,
    errorHandler: (error) => options.onError?.(error),
  })

  const resetIdleTimer = () => {
    window.clearTimeout(idleTimer)
    idleTimer = window.setTimeout(() => void stop(), idleTimeoutMilliseconds)
  }
  const onNavigation = () => record.addCustomEvent('navigation', { url: window.location.href })
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

  return { sessionID: session.id, stop }
}

async function createSession(options: ReplayCaptureOptions, sessionID: string): Promise<SessionResponse> {
  const response = await widgetFetch(options, 'replay/sessions', {
    method: 'POST',
    body: JSON.stringify({
      session_id: sessionID,
      widget_rule_id: options.rule.id,
      page_url: window.location.href,
      visitor_id: options.visitorID,
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
): Promise<void> {
  await retry(async () => {
    const declared = await widgetFetch(options, `replay/sessions/${sessionID}/batches`, {
      method: 'POST',
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
      credentials: 'omit',
    })
    if (!stored.ok) throw new Error(`replay batch upload failed with status ${stored.status}`)
    const completed = await widgetFetch(
      options,
      `replay/sessions/${sessionID}/batches/${batch.index}/complete`,
      { method: 'POST', body: JSON.stringify({ complete_token: upload.complete_token }) },
    )
		// A lost 204 response makes the retry observe an already-completed batch.
		if (!completed.ok && completed.status !== 409) {
			throw new Error(`replay batch completion failed with status ${completed.status}`)
		}
  })
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

async function retry(operation: () => Promise<void>): Promise<void> {
  let lastError: unknown
  for (const delay of retryDelays) {
    if (delay > 0) await new Promise((resolve) => window.setTimeout(resolve, delay))
    try {
      await operation()
      return
    } catch (error) {
      lastError = error
    }
  }
  throw lastError
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
