import { record } from 'rrweb'
import { startReplayCapture } from '../../src/modules/replay'
import { rule } from './fixtures'

const { mirrorGetNodeMock } = vi.hoisted(() => ({
  mirrorGetNodeMock: vi.fn<(id: number) => Node | null>(),
}))

vi.mock('rrweb', () => {
  const mockedRecord = vi.fn(() => vi.fn()) as ReturnType<typeof vi.fn> & {
    addCustomEvent: ReturnType<typeof vi.fn>
    mirror: { getNode: typeof mirrorGetNodeMock }
  }
  mockedRecord.addCustomEvent = vi.fn()
  mockedRecord.mirror = { getNode: mirrorGetNodeMock }
  return { record: mockedRecord }
})

const recordMock = vi.mocked(record)

describe('rrweb replay capture', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mirrorGetNodeMock.mockReset()
    window.history.replaceState({}, '', '/checkout')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('makes one sampling decision before creating a session', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const capture = await startReplayCapture({
      rule: replayRule({ replay_mode: 'sampling', replay_sampling_rate: 25 }),
      publicKey: 'pk_live_test', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
      random: () => 0.25,
    })
    expect(capture).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(recordMock).not.toHaveBeenCalled()
  })

  it('records masked rrweb events, uploads ordered batches, and finalizes', async () => {
    const requests: Array<{ url: string; init: RequestInit }> = []
    const fetchMock = vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      requests.push({ url, init })
      if (url.endsWith('/v1/replay/sessions?key=pk_live_test')) {
        return jsonResponse({ id: 'server-session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      if (url.endsWith('/batches?key=pk_live_test')) {
        const declaration = JSON.parse(String(init.body)) as { batch_index: number }
        return jsonResponse({
          presigned_url: `https://storage.example/batch-${declaration.batch_index}`,
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          complete_token: `token-${declaration.batch_index}`,
        }, 201)
      }
      if (url.startsWith('https://storage.example/')) return new Response(null, { status: 200 })
      if (url.includes('/complete?')) return new Response(null, { status: 204 })
      if (url.endsWith('/finalize?key=pk_live_test')) return jsonResponse({ status: 'processing' }, 202)
      throw new Error(`unexpected request ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const onError = vi.fn()
    const capture = await startReplayCapture({
      // A customer-configured mask selector must not narrow the anonymity-safe default.
      rule: replayRule({ replay_mask_text_selector: '.private' }),
      publicKey: 'pk_live_test', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
      visitorID: 'e8b3bb8c-e7ce-4c06-b804-c40f1db21355', onError,
    })
    expect(capture?.sessionID).toBe('server-session')
    const options = recordMock.mock.calls[0]?.[0]
    expect(options).toMatchObject({
      maskAllInputs: true, maskTextSelector: '*', blockSelector: '[data-fidibeki-block]',
      recordCanvas: false, inlineImages: false, collectFonts: false,
    })
    expect(typeof options?.maskTextFn).toBe('function')
    expect(options?.sampling).toBeUndefined()
    const emit = options?.emit as ((event: unknown) => void)
    for (let index = 0; index < 51; index++) {
      emit({ type: 3, timestamp: index + 1, data: { source: 3, id: 1, x: index, y: 0 } })
    }
    window.dispatchEvent(new Event('fidibeki:navigation'))
    expect(recordMock.addCustomEvent).toHaveBeenCalledWith(
      'navigation',
      { url: `${window.location.origin}/` },
    )

    await capture?.stop()
    expect(onError).not.toHaveBeenCalled()
    const sessionRequest = requests.find(({ url }) =>
      url.endsWith('/v1/replay/sessions?key=pk_live_test'))
    expect(JSON.parse(String(sessionRequest?.init.body))).toMatchObject({
      widget_rule_id: expect.any(String),
      visitor_id: 'e8b3bb8c-e7ce-4c06-b804-c40f1db21355',
      consent_token: 'consent-token',
    })
    const declarations = requests.filter(({ url }) => url.endsWith('/batches?key=pk_live_test'))
    expect(declarations).toHaveLength(2)
    expect(declarations.map(({ init }) => JSON.parse(String(init.body)).events_count)).toEqual([50, 1])
    expect(requests.filter(({ url }) => url.startsWith('https://storage.example/'))).toHaveLength(2)
		expect(requests[requests.length - 1]?.url).toContain('/finalize?key=pk_live_test')
  })

  it('discards unsent replay events when consent is revoked', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      requests.push(url)
      if (url.endsWith('/v1/replay/sessions?key=key')) {
        return jsonResponse({
          id: 'server-session',
          rrweb_version: '2.1.1',
          max_duration_seconds: 1800,
          max_events: 50000,
        }, 201)
      }
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(),
      publicKey: 'key',
      apiBase: new URL('https://widget.example/v1/'),
      consentToken: 'consent-token',
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({ type: 3, timestamp: 1, data: { source: 3, id: 1, x: 1, y: 1 } })

    capture?.revoke()
    await Promise.resolve()

    expect(requests).toEqual(['https://widget.example/v1/replay/sessions?key=key'])
  })

  it('forces input masking even when the widget rule requests it disabled', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule({ replay_mask_all_inputs: false }),
      publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    expect(recordMock.mock.calls[0]?.[0]).toMatchObject({ maskAllInputs: true })
    await capture?.stop()
  })

  it('projects session and navigation URLs to the configured safe prefix', async () => {
    window.history.replaceState(
      {},
      '',
      '/accounts/jane-private/orders/48291?token=secret&email=jane.doe%40example.com#fragment-secret',
    )
    const expectedSafeURL = `${window.location.origin}/accounts`
    const requests: Array<{ url: string; init: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      requests.push({ url, init })
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      if (url.endsWith('/finalize?key=key')) return jsonResponse({}, 202)
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule({
        matcher_type: 'url_prefix',
        matcher_value: expectedSafeURL,
      }),
      publicKey: 'key',
      apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const sessionRequest = requests.find(({ url }) => url.endsWith('/replay/sessions?key=key'))
    const sessionBody = JSON.parse(String(sessionRequest?.init.body)) as { page_url: string }
    expect(sessionBody.page_url).toBe(expectedSafeURL)
    expect(sessionBody.page_url).not.toContain('token')
    expect(sessionBody.page_url).not.toContain('jane.doe')
    expect(sessionBody.page_url).not.toContain('jane-private')
    expect(sessionBody.page_url).not.toContain('48291')
    expect(sessionBody.page_url).not.toContain('fragment-secret')

    window.dispatchEvent(new Event('fidibeki:navigation'))
    expect(recordMock.addCustomEvent).toHaveBeenCalledWith('navigation', { url: expectedSafeURL })
    await capture?.stop()
  })

  it('pins navigation events to the safe session location', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    // afterEach() calls vi.unstubAllGlobals(), so this override does not leak into later tests.
    vi.stubGlobal('location', { href: 'not a valid url' })
    recordMock.addCustomEvent.mockClear()
    window.dispatchEvent(new Event('fidibeki:navigation'))
    expect(recordMock.addCustomEvent).toHaveBeenCalledTimes(1)
    expect(recordMock.addCustomEvent).toHaveBeenCalledWith(
      'navigation',
      { url: 'http://localhost:3000/' },
    )
    await capture?.stop()
  })

  it('sanitizes identity-bearing attributes and text before an event reaches an uploaded batch', async () => {
    const requests: Array<{ url: string; init: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      requests.push({ url, init })
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      if (url.endsWith('/batches?key=key')) {
        const declaration = JSON.parse(String(init.body)) as { batch_index: number }
        return jsonResponse({
          presigned_url: `https://storage.example/batch-${declaration.batch_index}`,
          method: 'PUT', headers: {}, complete_token: `token-${declaration.batch_index}`,
        }, 201)
      }
      if (url.startsWith('https://storage.example/')) return new Response(null, { status: 200 })
      if (url.includes('/complete?')) return new Response(null, { status: 204 })
      if (url.includes('/finalize?')) return jsonResponse({}, 202)
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const live = document.createElement('img')
    live.className = 'avatar-jane'
    live.style.cssText = 'display:block;width:80px;height:80px;background-color:rgb(20, 30, 40)'
    document.body.append(live)
    mirrorGetNodeMock.mockImplementation(id => id === 9 ? live : null)
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 0,
          childNodes: [{
            type: 2, id: 9, tagName: 'img',
            attributes: {
              id: 'reporter-avatar', src: 'https://cdn.example.com/avatars/jane.doe@example.com.jpg',
              alt: 'Jane Doe', title: 'Jane Doe', 'data-reporter-email': 'jane.doe@example.com', class: 'avatar',
            },
            childNodes: [],
          }],
        },
      },
    })
    await capture?.stop()
    const uploadedBody = requests.find(({ url }) => url.startsWith('https://storage.example/'))?.init.body
    const serialized = await (uploadedBody as Blob).text()
    for (const sentinel of ['jane.doe@example.com', 'Jane Doe']) {
      expect(serialized).not.toContain(sentinel)
    }
    expect(serialized).toContain('"class":"avatar"')
    expect(serialized).toContain('"src":null')
  })

  it('suppresses canvas frames even when a legacy rule requests capture', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({
          id: 'canvas-session', rrweb_version: '2.1.1',
          max_duration_seconds: 1800, max_events: 50000,
        }, 201)
      }
      throw new Error(`unexpected request ${url}`)
    }))

    const onError = vi.fn()
    const capture = await startReplayCapture({
      rule: replayRule({ replay_record_canvas: true, replay_canvas_fps: 3 }),
      publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
      onError,
    })

    const recordOptions = recordMock.mock.calls[0]?.[0]
    expect(recordOptions).toMatchObject({
      recordCanvas: false,
      blockSelector: '[data-fidibeki-block]',
    })
    expect(recordOptions?.sampling).toBeUndefined()
    const canvasError = new Error('canvas capture is unavailable')
    recordOptions?.errorHandler?.(canvasError)
    expect(onError).toHaveBeenCalledWith(canvasError)
    await capture?.stop()
  })

  it('uploads active replay events before the browser lifecycle ends', async () => {
    vi.useFakeTimers()
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      requests.push(url)
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({
          id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000,
        }, 201)
      }
      if (url.endsWith('/batches?key=key')) {
        const declaration = JSON.parse(String(init.body)) as { batch_index: number }
        return jsonResponse({
          presigned_url: `https://storage.example/batch-${declaration.batch_index}`,
          method: 'PUT', headers: {}, complete_token: `token-${declaration.batch_index}`,
        }, 201)
      }
      if (url.startsWith('https://storage.example/')) return new Response(null, { status: 200 })
      if (url.includes('/complete?')) return new Response(null, { status: 204 })
      if (url.includes('/finalize?')) return jsonResponse({}, 202)
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({ type: 0, timestamp: 1, data: {} })

    await vi.advanceTimersByTimeAsync(30_000)

    expect(requests.filter((url) => url.endsWith('/batches?key=key'))).toHaveLength(1)
    await capture?.stop()
  })

  it('discards a queued batch when consent is revoked before upload starts', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      requests.push(url)
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({
          id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000,
        }, 201)
      }
      if (url.endsWith('/batches?key=key')) {
        return jsonResponse({
          presigned_url: 'https://storage.example/batch',
          method: 'PUT', headers: {}, complete_token: 'token',
        }, 201)
      }
      if (url === 'https://storage.example/batch') return new Response(null, { status: 200 })
      if (url.includes('/complete?')) return new Response(null, { status: 204 })
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    for (let index = 0; index < 50; index++) emit({ type: 0, timestamp: index, data: {} })

    capture?.revoke()
    await new Promise(resolve => window.setTimeout(resolve, 0))

    expect(requests.filter(url => url.endsWith('/batches?key=key'))).toHaveLength(0)
    expect(requests.filter(url => url.startsWith('https://storage.example/'))).toHaveLength(0)
  })

  it('aborts an in-flight batch request when consent is revoked', async () => {
    vi.useFakeTimers()
    let declarationCount = 0
    let declarationStarted = false
    let declarationSignal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({
          id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000,
        }, 201)
      }
      if (url.endsWith('/batches?key=key')) {
        declarationCount++
        declarationStarted = true
        declarationSignal = init.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          if (init.signal?.aborted) {
            reject(new DOMException('The operation was aborted', 'AbortError'))
            return
          }
          init.signal?.addEventListener('abort', () => {
            reject(new DOMException('The operation was aborted', 'AbortError'))
          }, { once: true })
        })
      }
      throw new Error(`unexpected request ${url}`)
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    for (let index = 0; index < 50; index++) emit({ type: 0, timestamp: index, data: {} })
    await vi.waitFor(() => expect(declarationStarted).toBe(true))

    capture?.revoke()
    expect((declarationSignal as AbortSignal | null)?.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(declarationCount).toBe(1)
  })

  it.each(['declaration', 'upload', 'completion'] as const)(
    'stops queued uploads after rate-limited %s without retrying',
    async (rejectedStage) => {
      vi.useFakeTimers()
      const attempts = { declaration: 0, upload: 0, completion: 0 }
      const onError = vi.fn()
      vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
        const url = input.toString()
        if (url.endsWith('/replay/sessions?key=key')) {
          return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
        }
        const stage = url.endsWith('/batches?key=key') ? 'declaration'
          : url.startsWith('https://storage.example/') ? 'upload' : 'completion'
        attempts[stage]++
        if (stage === rejectedStage) {
          return new Response(null, { status: 429, headers: { 'Retry-After': '60' } })
        }
        if (stage === 'declaration') {
          return jsonResponse({
            presigned_url: 'https://storage.example/batch', method: 'PUT', headers: {}, complete_token: 'token',
          }, 201)
        }
        return new Response(null, { status: stage === 'upload' ? 200 : 204 })
      }))
      const capture = await startReplayCapture({
        rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
        consentToken: 'consent-token', onError,
      })
      const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
      for (let index = 0; index < 101; index++) emit({ type: 0, timestamp: index, data: {} })
      const stopped = capture?.stop()
      await vi.advanceTimersByTimeAsync(2_000)
      await stopped

      expect(attempts[rejectedStage]).toBe(1)
      expect(attempts.declaration).toBe(1)
      expect(onError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ message: expect.stringContaining('429') }))
    },
  )

  it.each([401, 429, 503])('stops the recorder and reports a background upload failure once for status %s', async (status) => {
    vi.useFakeTimers()
    const onError = vi.fn()
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: URL | RequestInfo) => {
      const url = input.toString()
      requests.push(url)
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 1800, max_events: 50000 }, 201)
      }
      return new Response(null, { status })
    }))
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
      consentToken: 'consent-token', onError,
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    for (let index = 0; index < 101; index++) emit({ type: 0, timestamp: index, data: {} })
    await vi.advanceTimersByTimeAsync(2_000)

    expect(recordMock.mock.results[recordMock.mock.results.length - 1]?.value).toHaveBeenCalledOnce()
    expect(onError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ message: expect.stringContaining(String(status)) }))
    for (let index = 0; index < 50; index++) emit({ type: 0, timestamp: index, data: {} })
    await vi.advanceTimersByTimeAsync(60_000)
    await capture?.stop()
    expect(requests.filter(url => url.includes('/batches?'))).toHaveLength(status === 503 ? 3 : 1)
    expect(requests.some(url => url.includes('/finalize?'))).toBe(false)
    expect(onError).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['upload', 'completion'] as const)('retries only the failed %s without spending another declaration', async (failedStage) => {
    let declarations = 0
    let uploads = 0
    let completions = 0
    const fetchMock = vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      const url = input.toString()
      if (url.endsWith('/replay/sessions?key=key')) {
        return jsonResponse({ id: 'session', rrweb_version: '2.1.1', max_duration_seconds: 60, max_events: 10 }, 201)
      }
      if (url.endsWith('/batches?key=key')) {
        declarations++
        return jsonResponse({
          presigned_url: 'https://storage.example/batch', method: 'PUT', headers: {}, complete_token: 'token',
        }, 201)
      }
      if (url === 'https://storage.example/batch') {
        uploads++
        return new Response(null, { status: failedStage === 'upload' && uploads === 1 ? 503 : 200 })
      }
      if (url.includes('/complete?')) {
        completions++
        return new Response(null, { status: failedStage === 'completion' && completions === 1 ? 503 : 409 })
      }
      if (url.includes('/finalize?')) return jsonResponse({}, 202)
      throw new Error(`unexpected request ${url} ${String(init.body)}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({ type: 0, timestamp: 1, data: {} })
    await capture?.stop()
    expect(declarations).toBe(1)
    expect(uploads).toBe(failedStage === 'upload' ? 2 : 1)
    expect(completions).toBe(failedStage === 'completion' ? 2 : 1)
  })

  it('reports incompatible session responses without starting rrweb', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({
      id: 'session', rrweb_version: '1.0.0', max_duration_seconds: 1, max_events: 1,
    }, 201)))
    await expect(startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'), consentToken: 'consent-token',
    })).rejects.toThrow('incompatible')
    expect(recordMock).not.toHaveBeenCalled()
  })
})

function replayRule(overrides: Partial<ReturnType<typeof rule>> = {}) {
  return rule({
    replay_mode: 'full', replay_sampling_rate: 0,
    replay_max_duration_seconds: 1800, replay_max_events: 50000,
    replay_mask_all_inputs: true, replay_mask_text_selector: null,
    replay_record_canvas: false, replay_canvas_fps: 2,
    ...overrides,
  })
}

function jsonResponse(value: unknown, status: number): Response {
  return new Response(JSON.stringify(value), {
    status, headers: { 'Content-Type': 'application/json' },
  })
}
