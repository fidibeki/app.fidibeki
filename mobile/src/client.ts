import {
  MobileClientError,
  type AttachmentGrant,
  type AttachmentOutcome,
  type MobileCaptureSession,
  type MobileClientOptions,
  type MobileFeedbackAccepted,
  type MobileFeedbackRequest,
  type MobileSubmissionResult,
  type PreparedScreenshot,
} from './types'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'

const PRIVATE_UPLOAD_HEADERS = new Set(['authorization', 'cookie', 'proxy-authorization', 'x-api-key'])

function isLocalDevelopmentURL(url: URL): boolean {
  return url.protocol === 'http:' && ['localhost', '127.0.0.1', '::1', '10.0.2.2'].includes(url.hostname)
}

function normalizedBaseURL(rawURL: string): string {
  let url: URL
  try {
    url = new URL(rawURL)
  } catch {
    throw new TypeError('apiBaseUrl must be an absolute URL')
  }
  if (url.username || url.password) {
    throw new TypeError('apiBaseUrl must not include credentials')
  }
  if (url.protocol !== 'https:' && !isLocalDevelopmentURL(url)) {
    throw new TypeError('apiBaseUrl must use HTTPS outside a local development runtime')
  }
  url.pathname = url.pathname.replace(/\/+$/, '')
  url.search = ''
  url.hash = ''
  return url.toString().replace(/\/$/, '')
}

function stableValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stableValue)
  if (typeof value !== 'object' || value === null) return value
  return Object.fromEntries(
    Object.entries(value)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, child]) => [key, stableValue(child)]),
  )
}

export function canonicalRequest(request: MobileFeedbackRequest): string {
  const wireRequest: MobileFeedbackRequest = {
    client_feedback_id: request.client_feedback_id,
    responses: request.responses,
    ...(request.comment !== undefined ? { comment: request.comment } : {}),
    ...(request.reporter_email !== undefined ? { reporter_email: request.reporter_email } : {}),
    ...(request.notify_reporter !== undefined ? { notify_reporter: request.notify_reporter } : {}),
    ...(request.consent !== undefined
      ? {
          consent: {
            purpose: request.consent.purpose,
            notice_version: request.consent.notice_version,
            controller_profile_version: request.consent.controller_profile_version,
            widget_definition_version: request.consent.widget_definition_version,
            locale: request.consent.locale,
            granted: request.consent.granted,
          },
        }
      : {}),
    ...(request.screenshot !== undefined
      ? {
          screenshot: {
            client_attachment_id: request.screenshot.client_attachment_id,
            content_type: request.screenshot.content_type,
            size_bytes: request.screenshot.size_bytes,
          },
        }
      : {}),
    app_version: request.app_version,
    os_version: request.os_version,
    ...(request.device_locale !== undefined ? { device_locale: request.device_locale } : {}),
  }
  return JSON.stringify(stableValue(wireRequest))
}

async function parsedJSON(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch (error) {
    throw new MobileClientError('invalid_response', 'The server returned invalid JSON', {
      cause: error,
    })
  }
}

function isRetryableStatus(status: number): boolean {
  return status === 408 || status === 429 || status >= 500
}

function errorFromStatus(status: number): MobileClientError | null {
  if (status === 401 || status === 403 || status === 404) {
    return new MobileClientError('access_denied', 'The capture session is not available')
  }
  if (status === 410) {
    return new MobileClientError('session_expired', 'The capture session expired')
  }
  return null
}

function safeValidationFields(
  details: unknown,
  session: MobileCaptureSession,
): Record<string, string> {
  const allowed = new Set([
    'comment',
    'reporter_email',
    'consent',
    ...session.configuration.definition.response_blocks.map((block) => `responses.${block.key}`),
  ])
  const fields: Record<string, string> = {}
  const add = (field: unknown, message: unknown): void => {
    if (typeof field !== 'string' || typeof message !== 'string' || !allowed.has(field)) return
    fields[field] = field === 'consent' ? 'required' : 'invalid'
  }
  if (Array.isArray(details)) {
    for (const detail of details) {
      if (typeof detail === 'object' && detail !== null) {
        const value = detail as Record<string, unknown>
        add(value.field, value.message)
      }
    }
  } else if (typeof details === 'object' && details !== null) {
    const rawFields = (details as Record<string, unknown>).fields
    if (typeof rawFields === 'object' && rawFields !== null) {
      for (const [field, message] of Object.entries(rawFields)) add(field, message)
    }
  }
  return fields
}

function errorFromResponse(
  status: number,
  body: unknown,
  session: MobileCaptureSession,
): MobileClientError {
  const statusError = errorFromStatus(status)
  if (statusError) return statusError
  const envelope = typeof body === 'object' && body !== null ? (body as Record<string, unknown>) : {}
  const object =
    typeof envelope.error === 'object' && envelope.error !== null ? (envelope.error as Record<string, unknown>) : null
  if (!object) {
    if (isRetryableStatus(status)) {
      return new MobileClientError('network_error', 'The server returned an invalid retryable error', {
        retryable: true,
      })
    }
    return new MobileClientError('invalid_response', 'The server returned an invalid error')
  }
  const code = typeof object.code === 'string' ? object.code : ''
  const reason = typeof object.reason === 'string' ? object.reason : ''
  if (status === 409 && code === 'configuration_changed') {
    return new MobileClientError('configuration_changed', 'The feedback configuration changed')
  }
  if (status === 422) {
    return new MobileClientError('validation_error', 'Feedback is invalid', {
      fields: safeValidationFields(object.details, session),
    })
  }
  if (code.includes('session_expired') || reason === 'session_expired') {
    return new MobileClientError('session_expired', 'The capture session expired')
  }
  return new MobileClientError('network_error', 'Feedback request failed', {
    retryable: isRetryableStatus(status),
  })
}

function parseAccepted(body: unknown): MobileFeedbackAccepted {
  if (typeof body !== 'object' || body === null) {
    throw new MobileClientError('invalid_response', 'Missing accepted feedback')
  }
  const value = body as Record<string, unknown>
  const attachments = value.attachments
  if (
    typeof value.feedback_id !== 'string' ||
    value.feedback_id === '' ||
    typeof value.feedback_public_id !== 'string' ||
    value.feedback_public_id === '' ||
    !Array.isArray(attachments)
  ) {
    throw new MobileClientError('invalid_response', 'Invalid accepted feedback')
  }
  const grants: AttachmentGrant[] = attachments.map((raw) => {
    if (typeof raw !== 'object' || raw === null) {
      throw new MobileClientError('invalid_response', 'Invalid attachment grant')
    }
    const grant = raw as Record<string, unknown>
    if (
      typeof grant.attachment_id !== 'string' ||
      typeof grant.client_attachment_id !== 'string' ||
      typeof grant.presigned_url !== 'string' ||
      grant.method !== 'PUT' ||
      typeof grant.complete_token !== 'string' ||
      typeof grant.expires_at !== 'string' ||
      typeof grant.headers !== 'object' ||
      grant.headers === null
    ) {
      throw new MobileClientError('invalid_response', 'Invalid attachment grant')
    }
    for (const [name, headerValue] of Object.entries(grant.headers)) {
      if (PRIVATE_UPLOAD_HEADERS.has(name.toLowerCase()) || typeof headerValue !== 'string') {
        throw new MobileClientError('invalid_response', 'Unsafe attachment upload headers')
      }
    }
    return raw as AttachmentGrant
  })
  return {
    feedback_id: value.feedback_id,
    feedback_public_id: value.feedback_public_id,
    attachments: grants,
  }
}

export class MobileClient {
  private static readonly MAX_RETAINED_ATTEMPTS = 32
  private static readonly BOUND_RETRY_MS = 10 * 60 * 1000
  private readonly apiBaseUrl: string
  private readonly fetchImplementation: typeof globalThis.fetch
  private readonly attempts = new Map<
    string,
    {
      digest: string
      expiresAt: number
      inFlight: Promise<MobileFeedbackAccepted> | null
      retain: boolean
      bound: boolean
    }
  >()

  constructor(options: MobileClientOptions) {
    this.apiBaseUrl = normalizedBaseURL(options.apiBaseUrl)
    const fetchImplementation = options.fetch ?? globalThis.fetch
    if (!fetchImplementation) throw new TypeError('A fetch implementation is required')
    this.fetchImplementation = fetchImplementation
  }

  async submitFeedback(session: MobileCaptureSession, request: MobileFeedbackRequest): Promise<MobileFeedbackAccepted> {
    const serialized = canonicalRequest(request)
    const digest = bytesToHex(sha256(utf8ToBytes(serialized)))
    const now = Date.now()
    for (const [id, attempt] of this.attempts) {
      if (attempt.inFlight === null && (!attempt.retain || now >= attempt.expiresAt)) {
        this.attempts.delete(id)
      }
    }
    let existing = this.attempts.get(request.client_feedback_id)
    if (existing && existing.digest !== digest) {
      throw new MobileClientError('retry_payload_changed', 'An idempotent retry must use the exact original request')
    }
    if (!existing) {
      const expiresAt = Date.parse(session.submit_expires_at)
      if (now >= expiresAt) {
        throw new MobileClientError('session_expired', 'The capture session expired')
      }
      if (this.attempts.size >= MobileClient.MAX_RETAINED_ATTEMPTS) {
        throw new MobileClientError('network_error', 'Too many feedback requests are awaiting a definitive result', {
          retryable: true,
        })
      }
      existing = {
        digest,
        expiresAt,
        inFlight: null,
        retain: true,
        bound: false,
      }
      this.attempts.set(request.client_feedback_id, existing)
    }
    const retainAmbiguousSuccess = (cause: unknown): MobileClientError => {
      existing.retain = true
      if (!existing.bound) {
        existing.bound = true
        existing.expiresAt = Date.now() + MobileClient.BOUND_RETRY_MS
      }
      return new MobileClientError('invalid_response', 'The server returned an invalid success response', {
        retryable: true,
        cause,
      })
    }
    const retainRetryableFailure = (error: MobileClientError): MobileClientError => {
      existing.retain = true
      if (!existing.bound) {
        existing.bound = true
        existing.expiresAt = Date.now() + MobileClient.BOUND_RETRY_MS
      }
      return error
    }
    if (existing.inFlight) return existing.inFlight
    const operation = (async () => {
      let response: Response
      try {
        response = await this.fetchImplementation(`${this.apiBaseUrl}/v1/mobile/feedback`, {
          method: 'POST',
          headers: {
            authorization: `Bearer ${session.token}`,
            'content-type': 'application/json',
          },
          body: serialized,
        })
      } catch (error) {
        existing.retain = true
        if (!existing.bound) {
          existing.bound = true
          existing.expiresAt = Date.now() + MobileClient.BOUND_RETRY_MS
        }
        throw new MobileClientError('network_error', 'Feedback request failed', {
          retryable: true,
          cause: error,
        })
      }
      let body: unknown
      try {
        body = await parsedJSON(response)
      } catch (error) {
        if (response.ok) throw retainAmbiguousSuccess(error)
        const statusError = errorFromStatus(response.status)
        if (statusError) {
          existing.retain = false
          throw statusError
        }
        if (isRetryableStatus(response.status)) {
          throw retainRetryableFailure(
            new MobileClientError('network_error', 'The server returned an invalid retryable error', {
              retryable: true,
              cause: error,
            }),
          )
        }
        existing.retain = existing.bound
        throw error
      }
      if (!response.ok) {
        const responseError = errorFromResponse(response.status, body, session)
        if (responseError.retryable) throw retainRetryableFailure(responseError)
        existing.retain = false
        throw responseError
      }
      let accepted: MobileFeedbackAccepted
      try {
        accepted = parseAccepted(body)
      } catch (error) {
        throw retainAmbiguousSuccess(error)
      }
      existing.retain = true
      if (!existing.bound) {
        existing.bound = true
        existing.expiresAt = Date.now() + MobileClient.BOUND_RETRY_MS
      }
      return accepted
    })()
    existing.inFlight = operation
    try {
      return await operation
    } finally {
      if (existing.inFlight === operation) existing.inFlight = null
      if (!existing.retain) {
        this.attempts.delete(request.client_feedback_id)
      }
    }
  }

  async uploadScreenshot(
    session: MobileCaptureSession,
    accepted: MobileFeedbackAccepted,
    screenshot: PreparedScreenshot,
  ): Promise<AttachmentOutcome> {
    const grant = accepted.attachments.find(
      (candidate) => candidate.client_attachment_id === screenshot.clientAttachmentId,
    )
    if (!grant) {
      return {
        attachmentId: screenshot.clientAttachmentId,
        status: 'failed',
        stage: 'upload',
        error: 'The server did not grant this attachment',
      }
    }
    let uploadURL: URL
    try {
      uploadURL = new URL(grant.presigned_url)
    } catch {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'upload',
        error: 'The upload URL was invalid',
      }
    }
    if (
      (uploadURL.protocol !== 'https:' && !isLocalDevelopmentURL(uploadURL)) ||
      uploadURL.username ||
      uploadURL.password
    ) {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'upload',
        error: 'The upload URL was invalid',
      }
    }
    const uploadHeaders = new Headers(grant.headers)
    uploadHeaders.set('content-type', screenshot.contentType)
    let uploadResponse: Response
    try {
      uploadResponse = await this.fetchImplementation(grant.presigned_url, {
        method: grant.method,
        headers: uploadHeaders,
        body: screenshot.blob,
      })
    } catch (error) {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'upload',
        error: error instanceof Error ? error.message : 'Upload failed',
      }
    }
    if (!uploadResponse.ok) {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'upload',
        error: `Upload failed with status ${uploadResponse.status}`,
      }
    }
    let completionResponse: Response
    try {
      completionResponse = await this.fetchImplementation(
        `${this.apiBaseUrl}/v1/mobile/attachments/${encodeURIComponent(grant.attachment_id)}/complete`,
        {
          method: 'POST',
          headers: {
            authorization: `Bearer ${session.token}`,
            'content-type': 'application/json',
          },
          body: JSON.stringify({ complete_token: grant.complete_token }),
        },
      )
    } catch (error) {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'complete',
        error: error instanceof Error ? error.message : 'Completion failed',
      }
    }
    if (!completionResponse.ok) {
      return {
        attachmentId: grant.attachment_id,
        status: 'failed',
        stage: 'complete',
        error: `Completion failed with status ${completionResponse.status}`,
      }
    }
    return { attachmentId: grant.attachment_id, status: 'uploaded' }
  }

  async submit(
    session: MobileCaptureSession,
    request: MobileFeedbackRequest,
    options: {
      screenshot?: PreparedScreenshot
      onAccepted?: (accepted: MobileFeedbackAccepted) => void | Promise<void>
    } = {},
  ): Promise<MobileSubmissionResult> {
    const accepted = await this.submitFeedback(session, request)
    await options.onAccepted?.(accepted)
    const attachments: AttachmentOutcome[] = []
    if (options.screenshot) {
      attachments.push(await this.uploadScreenshot(session, accepted, options.screenshot))
    }
    return {
      feedbackId: accepted.feedback_id,
      feedbackPublicId: accepted.feedback_public_id,
      clientFeedbackId: request.client_feedback_id,
      accepted: true,
      attachments,
    }
  }
}
