import { record } from 'rrweb'
import { startReplayCapture } from '../../src/modules/replay'
import { rule } from './fixtures'

vi.mock('rrweb', () => {
  const mockedRecord = vi.fn(() => vi.fn()) as ReturnType<typeof vi.fn> & {
    addCustomEvent: ReturnType<typeof vi.fn>
  }
  mockedRecord.addCustomEvent = vi.fn()
  return { record: mockedRecord }
})

const recordMock = vi.mocked(record)

describe('rrweb replay capture', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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
      publicKey: 'pk_live_test', apiBase: new URL('https://widget.example/v1/'),
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
      rule: replayRule({ replay_mask_text_selector: '.private' }),
      publicKey: 'pk_live_test', apiBase: new URL('https://widget.example/v1/'),
      visitorID: 'e8b3bb8c-e7ce-4c06-b804-c40f1db21355', onError,
    })
    expect(capture?.sessionID).toBe('server-session')
    const options = recordMock.mock.calls[0]?.[0]
    expect(options).toMatchObject({
      maskAllInputs: true, maskTextSelector: '.private', blockSelector: '[data-fidibeki-block]',
      recordCanvas: false, inlineImages: false, collectFonts: false,
    })
    expect(options?.sampling).toBeUndefined()
    const emit = options?.emit as ((event: unknown) => void)
    for (let index = 0; index < 51; index++) emit({ type: 3, timestamp: index, data: { index } })
    window.dispatchEvent(new Event('fidibeki:navigation'))
    expect(recordMock.addCustomEvent).toHaveBeenCalledWith('navigation', { url: window.location.href })

    await capture?.stop()
    expect(onError).not.toHaveBeenCalled()
    const sessionRequest = requests.find(({ url }) =>
      url.endsWith('/v1/replay/sessions?key=pk_live_test'))
    expect(JSON.parse(String(sessionRequest?.init.body))).toMatchObject({
      widget_rule_id: expect.any(String),
      visitor_id: 'e8b3bb8c-e7ce-4c06-b804-c40f1db21355',
    })
    const declarations = requests.filter(({ url }) => url.endsWith('/batches?key=pk_live_test'))
    expect(declarations).toHaveLength(2)
    expect(declarations.map(({ init }) => JSON.parse(String(init.body)).events_count)).toEqual([50, 1])
    expect(requests.filter(({ url }) => url.startsWith('https://storage.example/'))).toHaveLength(2)
		expect(requests[requests.length - 1]?.url).toContain('/finalize?key=pk_live_test')
  })

  it('records canvas frames only after explicit opt-in and uses the configured FPS', async () => {
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
      publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
      onError,
    })

    const recordOptions = recordMock.mock.calls[0]?.[0]
    expect(recordOptions).toMatchObject({
      recordCanvas: true,
      sampling: { canvas: 3 },
      blockSelector: '[data-fidibeki-block]',
    })
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
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({ type: 2, timestamp: 1, data: {} })

    await vi.advanceTimersByTimeAsync(30_000)

    expect(requests.filter((url) => url.endsWith('/batches?key=key'))).toHaveLength(1)
    await capture?.stop()
  })

  it('retries a failed upload and treats an already-complete response as success', async () => {
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
        return new Response(null, { status: uploads === 1 ? 503 : 200 })
      }
      if (url.includes('/complete?')) {
        completions++
        return new Response(null, { status: 409 })
      }
      if (url.includes('/finalize?')) return jsonResponse({}, 202)
      throw new Error(`unexpected request ${url} ${String(init.body)}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    const capture = await startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
    })
    const emit = recordMock.mock.calls[0]?.[0]?.emit as ((event: unknown) => void)
    emit({ type: 2, timestamp: 1, data: {} })
    await capture?.stop()
    expect(declarations).toBe(2)
    expect(uploads).toBe(2)
    expect(completions).toBe(1)
  })

  it('reports incompatible session responses without starting rrweb', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({
      id: 'session', rrweb_version: '1.0.0', max_duration_seconds: 1, max_events: 1,
    }, 201)))
    await expect(startReplayCapture({
      rule: replayRule(), publicKey: 'key', apiBase: new URL('https://widget.example/v1/'),
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
