import {
  MobileClient,
  MobileClientError,
  canonicalRequest,
  type MobileFeedbackRequest,
  type PreparedScreenshot,
} from '../src'
import { session } from './fixtures'

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

const clientFeedbackID = '11111111-1111-4111-8111-111111111111'
const clientAttachmentID = '22222222-2222-4222-8222-222222222222'

function request(overrides: Partial<MobileFeedbackRequest> = {}): MobileFeedbackRequest {
  return {
    client_feedback_id: clientFeedbackID,
    responses: { rating: 5, feedback: 'Useful checkout' },
    app_version: '1.2.3',
    os_version: '18.5',
    ...overrides,
  }
}

function accepted() {
  return {
    feedback_id: 'feedback-uuid',
    feedback_public_id: 'FDBK-42',
    attachments: [
      {
        attachment_id: 'attachment-uuid',
        client_attachment_id: clientAttachmentID,
        presigned_url: 'https://objects.example.test/upload',
        method: 'PUT',
        headers: { 'x-amz-meta-purpose': 'feedback' },
        complete_token: 'complete-secret',
        expires_at: '2099-07-28T12:10:00Z',
      },
    ],
  }
}

describe('mobile backend wire contract', () => {
  it('serializes only the strict submit schema including singular screenshot and consent', () => {
    const value = request({
      reporter_email: 'reporter@example.test',
      notify_reporter: true,
      consent: {
        purpose: 'feedback_updates',
        notice_version: '2026-07-controller-v1',
        controller_profile_version: 3,
        widget_definition_version: 2,
        locale: 'en',
        granted: true,
      },
      screenshot: {
        client_attachment_id: clientAttachmentID,
        content_type: 'image/png',
        size_bytes: 3,
      },
    }) as MobileFeedbackRequest & {
      context_url?: string
      screen?: string
      application_id?: string
      analytics?: object
    }
    value.context_url = 'https://should-not-leave.example'
    value.screen = '/private'
    value.application_id = 'com.private.app'
    value.analytics = { device_id: 'forbidden' }
    const wire = JSON.parse(canonicalRequest(value)) as Record<string, unknown>
    expect(Object.keys(wire).sort()).toEqual([
      'app_version',
      'client_feedback_id',
      'consent',
      'notify_reporter',
      'os_version',
      'reporter_email',
      'responses',
      'screenshot',
    ])
    expect(wire).not.toHaveProperty('attachments')
    expect(JSON.stringify(wire)).not.toContain('should-not-leave')
    expect(JSON.stringify(wire)).not.toContain('device_id')
  })

  it('uses the exact accepted/grant/completion contract without leaking bearer or URI', async () => {
    const calls: Array<[RequestInfo | URL, RequestInit | undefined]> = []
    const fetchMock: typeof fetch = vi.fn(async (input, init) => {
      calls.push([input, init])
      if (calls.length === 1) return response(accepted())
      if (calls.length === 2) return new Response(null, { status: 200 })
      return response({ attachment_id: 'attachment-uuid', status: 'accepted' })
    })
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })
    const screenshot: PreparedScreenshot = {
      clientAttachmentId: clientAttachmentID,
      contentType: 'image/png',
      size: 3,
      blob: new Blob(['png'], { type: 'image/png' }),
      dispose: vi.fn(async () => undefined),
    }
    const result = await client.submit(
      session(),
      request({
        screenshot: {
          client_attachment_id: clientAttachmentID,
          content_type: 'image/png',
          size_bytes: 3,
        },
      }),
      { screenshot },
    )
    expect(result).toEqual({
      feedbackId: 'feedback-uuid',
      feedbackPublicId: 'FDBK-42',
      clientFeedbackId: clientFeedbackID,
      accepted: true,
      attachments: [{ attachmentId: 'attachment-uuid', status: 'uploaded' }],
    })
    expect(calls.map(([url]) => String(url))).toEqual([
      'https://api.example.test/v1/mobile/feedback',
      'https://objects.example.test/upload',
      'https://api.example.test/v1/mobile/attachments/attachment-uuid/complete',
    ])
    expect(new Headers(calls[0]![1]?.headers).get('authorization')).toContain('fmc_')
    expect(new Headers(calls[1]![1]?.headers).has('authorization')).toBe(false)
    expect(calls[1]![1]?.method).toBe('PUT')
    expect(calls[2]![1]?.body).toBe('{"complete_token":"complete-secret"}')
    expect(JSON.stringify(calls)).not.toContain('file:///')
  })

  it('keeps an exact bound retry past submit expiry without retaining payload PII', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-28T12:00:00Z'))
    try {
      const current = session({
        submit_expires_at: new Date(Date.now() + 1_000).toISOString(),
      })
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockRejectedValueOnce(new Error('connection reset'))
        .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
        .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: fetchMock,
      })
      await expect(client.submitFeedback(current, request())).rejects.toMatchObject({
        code: 'network_error',
        retryable: true,
      })
      await expect(client.submitFeedback(current, request({ comment: 'changed' }))).rejects.toMatchObject({
        code: 'retry_payload_changed',
      })
      await expect(client.submitFeedback(current, request())).resolves.toMatchObject({
        feedback_id: 'feedback-uuid',
      })
      vi.setSystemTime(new Date('2026-07-28T12:00:02Z'))
      await expect(
        client.submitFeedback(current, request({ comment: 'changed after acceptance' })),
      ).rejects.toMatchObject({ code: 'retry_payload_changed' })
      await expect(client.submitFeedback(current, request())).resolves.toMatchObject({
        feedback_id: 'feedback-uuid',
      })
      expect(fetchMock).toHaveBeenCalledTimes(3)
    } finally {
      vi.useRealTimers()
    }
  })

  it('releases definitive validation failures so the user can edit and retry', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        response(
          {
            error: {
              code: 'validation_failed',
              message: 'invalid',
              details: [{ field: 'comment', message: 'is invalid' }],
            },
          },
          422,
        ),
      )
      .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })
    await expect(client.submitFeedback(session(), request({ comment: 'bad' }))).rejects.toMatchObject({
      code: 'validation_error',
    })
    await expect(client.submitFeedback(session(), request({ comment: 'fixed' }))).resolves.toMatchObject({
      feedback_id: 'feedback-uuid',
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('retains an ambiguous 2xx digest for an exact retry after submit expiry', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-28T12:00:00Z'))
    try {
      const current = session({
        submit_expires_at: new Date(Date.now() + 1_000).toISOString(),
      })
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(new Response('{', { status: 200 }))
        .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: fetchMock,
      })
      await expect(client.submitFeedback(current, request())).rejects.toMatchObject({
        code: 'invalid_response',
        retryable: true,
      })
      vi.setSystemTime(new Date('2026-07-28T12:00:02Z'))
      await expect(client.submitFeedback(current, request({ comment: 'changed' }))).rejects.toMatchObject({
        code: 'retry_payload_changed',
      })
      await expect(client.submitFeedback(current, request())).resolves.toMatchObject({
        feedback_id: 'feedback-uuid',
      })
      expect(fetchMock).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it.each([
    ['invalid JSON', new Response('{', { status: 503 })],
    ['a malformed envelope', response({ message: 'upstream unavailable' }, 503)],
  ])('retains a retryable non-2xx digest past submit expiry for %s', async (_case, failure) => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-28T12:00:00Z'))
    try {
      const current = session({
        submit_expires_at: new Date(Date.now() + 1_000).toISOString(),
      })
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(failure)
        .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: fetchMock,
      })
      await expect(client.submitFeedback(current, request())).rejects.toMatchObject({
        code: 'network_error',
        retryable: true,
      })
      vi.setSystemTime(new Date('2026-07-28T12:00:02Z'))
      await expect(client.submitFeedback(current, request({ comment: 'changed' }))).rejects.toMatchObject({
        code: 'retry_payload_changed',
      })
      await expect(client.submitFeedback(current, request())).resolves.toMatchObject({
        feedback_id: 'feedback-uuid',
      })
      expect(fetchMock).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('bounds ambiguous request retention without evicting active retry evidence', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockRejectedValue(new Error('offline'))
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })
    for (let index = 0; index < 33; index += 1) {
      const id = `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
      await expect(
        client.submitFeedback(session(), request({ client_feedback_id: id, comment: `payload-${index}` })),
      ).rejects.toMatchObject({ code: 'network_error' })
    }
    await expect(
      client.submitFeedback(
        session(),
        request({
          client_feedback_id: '00000000-0000-4000-8000-000000000000',
          comment: 'edited after eviction',
        }),
      ),
    ).rejects.toMatchObject({ code: 'retry_payload_changed' })
    expect(fetchMock).toHaveBeenCalledTimes(32)
  })

  it('coalesces concurrent attempts with the same id and payload', async () => {
    let resolveAttempt!: (response: Response) => void
    const deferred = new Promise<Response>((resolve) => {
      resolveAttempt = resolve
    })
    const fetchMock = vi.fn<typeof fetch>(async () => deferred)
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })

    const first = client.submitFeedback(session(), request())
    const second = client.submitFeedback(session(), request())
    expect(fetchMock).toHaveBeenCalledOnce()
    resolveAttempt(response({ ...accepted(), attachments: [] }))
    await expect(first).resolves.toMatchObject({ feedback_id: 'feedback-uuid' })
    await expect(second).resolves.toMatchObject({ feedback_id: 'feedback-uuid' })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('releases a prior ambiguity after an exact retry gets a definitive failure', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new Error('upstream unavailable'))
      .mockResolvedValueOnce(
        response(
          {
            error: {
              code: 'validation_failed',
              message: 'invalid',
              details: [{ field: 'comment', message: 'is invalid' }],
            },
          },
          422,
        ),
      )
      .mockResolvedValueOnce(response({ ...accepted(), attachments: [] }))
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })

    await expect(client.submitFeedback(session(), request())).rejects.toMatchObject({
      code: 'network_error',
      retryable: true,
    })
    await expect(client.submitFeedback(session(), request())).rejects.toMatchObject({
      code: 'validation_error',
    })
    await expect(client.submitFeedback(session(), request({ comment: 'fixed' }))).resolves.toMatchObject({
      feedback_id: 'feedback-uuid',
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('returns accepted feedback with a failed upload outcome and never resubmits it', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(response(accepted()))
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: fetchMock,
    })
    const screenshot: PreparedScreenshot = {
      clientAttachmentId: clientAttachmentID,
      contentType: 'image/png',
      size: 3,
      blob: new Blob(['png'], { type: 'image/png' }),
      dispose: async () => undefined,
    }
    const result = await client.submit(
      session(),
      request({
        screenshot: {
          client_attachment_id: clientAttachmentID,
          content_type: 'image/png',
          size_bytes: 3,
        },
      }),
      { screenshot },
    )
    expect(result.accepted).toBe(true)
    expect(result.attachments).toEqual([
      {
        attachmentId: 'attachment-uuid',
        status: 'failed',
        stage: 'upload',
        error: 'Upload failed with status 503',
      },
    ])
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('rejects cleartext or credentialed upload destinations outside local development', async () => {
    const screenshot: PreparedScreenshot = {
      clientAttachmentId: clientAttachmentID,
      contentType: 'image/png',
      size: 3,
      blob: new Blob(['png'], { type: 'image/png' }),
      dispose: async () => undefined,
    }
    for (const presignedURL of [
      'http://objects.example.test/upload',
      'https://user:secret@objects.example.test/upload',
    ]) {
      const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
        response({
          ...accepted(),
          attachments: [{ ...accepted().attachments[0], presigned_url: presignedURL }],
        }),
      )
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: fetchMock,
      })
      const result = await client.submit(
        session(),
        request({
          screenshot: {
            client_attachment_id: clientAttachmentID,
            content_type: 'image/png',
            size_bytes: 3,
          },
        }),
        { screenshot },
      )
      expect(result.attachments).toEqual([
        {
          attachmentId: 'attachment-uuid',
          status: 'failed',
          stage: 'upload',
          error: 'The upload URL was invalid',
        },
      ])
      expect(fetchMock).toHaveBeenCalledTimes(1)
    }
  })

  it.each([
    [
      409,
      { error: { code: 'configuration_changed', message: 'changed' } },
      { code: 'configuration_changed', retryable: false },
    ],
    [
      422,
      {
        error: {
          code: 'validation_failed',
          message: 'invalid',
          details: [{ field: 'responses.feedback', message: 'is required' }],
        },
      },
      {
        code: 'validation_error',
        fields: { 'responses.feedback': 'is required' },
        retryable: false,
      },
    ],
    [
      422,
      {
        error: {
          code: 'validation_failed',
          message: 'invalid',
          details: { fields: { display_locale: 'is unsupported' } },
        },
      },
      {
        code: 'validation_error',
        fields: { display_locale: 'is unsupported' },
        retryable: false,
      },
    ],
    [
      429,
      {
        error: {
          code: 'rate_limited',
          message: 'slow down',
          retry_after_seconds: 10,
        },
      },
      { code: 'network_error', retryable: true },
    ],
    [503, { error: { code: 'service_unavailable', message: 'later' } }, { code: 'network_error', retryable: true }],
  ])('parses backend error envelope for HTTP %i', async (status, body, expected) => {
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: vi.fn(async () => response(body, status)),
    })
    await expect(client.submitFeedback(session(), request())).rejects.toMatchObject(expected)
  })

  it.each([
    [401, 'access_denied'],
    [403, 'access_denied'],
    [404, 'access_denied'],
    [410, 'session_expired'],
  ])('maps terminal HTTP %i without requiring a valid error envelope', async (status, code) => {
    for (const failure of [new Response('{', { status }), response({ message: 'proxy response' }, status)]) {
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: vi.fn(async () => failure),
      })
      await expect(client.submitFeedback(session(), request())).rejects.toMatchObject({ code })
    }
  })

  it('rejects malformed success and error JSON', async () => {
    for (const status of [200, 500]) {
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: vi.fn(async () => new Response('{', { status })),
      })
      await expect(client.submitFeedback(session(), request())).rejects.toMatchObject({
        code: status === 200 ? 'invalid_response' : 'network_error',
        retryable: true,
      })
    }
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: vi.fn(async () => response({ message: 'wrong envelope' }, 500)),
    })
    await expect(client.submitFeedback(session(), request())).rejects.toBeInstanceOf(MobileClientError)
  })

  it('rejects bearer-like object upload headers from a malformed grant', async () => {
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: vi.fn(async () =>
        response({
          ...accepted(),
          attachments: [
            {
              ...accepted().attachments[0],
              headers: { authorization: 'must-not-forward' },
            },
          ],
        }),
      ),
    })
    await expect(client.submitFeedback(session(), request())).rejects.toMatchObject({
      code: 'invalid_response',
    })
  })
})
