import React from 'react'
import { Pressable } from 'react-native'
import TestRenderer, { act, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer'
import {
  FidibekiProvider,
  recurrenceStorageKey,
  useFidibekiFeedback,
  type MobileCaptureSession,
  type ScreenshotProvider,
  type SessionProvider,
  type StorageAdapter,
} from '../src'
import { definition, MemoryStorage, session } from './fixtures'

interface NativeTestControls {
  Platform: { OS: 'ios' | 'android' }
  __setPlatform(platform: 'ios' | 'android'): void
  __setReducedMotion(enabled: boolean): void
  __setColorScheme(colorScheme: 'light' | 'dark' | null): void
  __emitAppState(state: 'active' | 'background'): void
}

async function flush(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve))
}

function HostFeedbackTrigger() {
  const feedback = useFidibekiFeedback()
  return (
    <Pressable
      accessibilityLabel="Open product feedback"
      disabled={!feedback.eligible}
      onPress={() => void feedback.open()}
      testID="host-feedback-trigger"
    />
  )
}

async function renderSDK(
  options: {
    sessionProvider?: SessionProvider
    screenshotProvider?: ScreenshotProvider
    storage?: StorageAdapter
    screen?: string
  } = {},
): Promise<ReactTestRenderer> {
  const provider = options.sessionProvider ?? vi.fn(async () => session())
  let renderer!: ReactTestRenderer
  await act(async () => {
    renderer = TestRenderer.create(
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        locale="en"
        osVersion="18.5"
        screen={options.screen ?? '/checkout/review?private=true'}
        sessionProvider={provider}
        storage={options.storage ?? new MemoryStorage()}
        {...(options.screenshotProvider ? { screenshotProvider: options.screenshotProvider } : {})}
      >
        <HostFeedbackTrigger />
      </FidibekiProvider>,
    )
    await flush()
  })
  return renderer
}

async function openSheet(renderer: ReactTestRenderer): Promise<void> {
  const trigger = renderer.root.findByProps({ testID: 'host-feedback-trigger' })
  await act(async () => {
    trigger.props.onPress()
    await flush()
  })
}

function pressByLabel(root: ReactTestInstance, label: string): void {
  root.findByProps({ accessibilityLabel: label }).props.onPress()
}

async function fillRequiredForm(renderer: ReactTestRenderer): Promise<void> {
  await act(async () => {
    pressByLabel(renderer.root, '5 out of 5')
    pressByLabel(renderer.root, 'Alpha')
    renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Captured on the current screen')
  })
}

describe('native provider, host trigger, and modal sheet', () => {
  beforeEach(async () => {
    const native = (await import('react-native')) as unknown as NativeTestControls
    native.__setReducedMotion(false)
    native.__setColorScheme('light')
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              feedback_id: 'feedback-id',
              feedback_public_id: 'FDBK-42',
              attachments: [],
            }),
            { status: 200 },
          ),
      ),
    )
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it.each(['ios', 'android'] as const)(
    'renders every response block with accessible native controls on %s',
    async (platform) => {
      const native = (await import('react-native')) as unknown as NativeTestControls
      native.__setPlatform(platform)
      const renderer = await renderSDK()
      await openSheet(renderer)
      expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(true)
      expect(renderer.root.findByProps({ testID: `fidibeki-keyboard-${platform}` }).props.behavior).toBe(
        platform === 'ios' ? 'padding' : 'height',
      )
      expect(renderer.root.findAll((node) => String(node.type) === 'SafeAreaView')).toHaveLength(1)
      expect(
        renderer.root.findAll((node) => String(node.type) === 'View' && node.props.accessibilityRole === 'radiogroup'),
      ).toHaveLength(3)
      expect(
        renderer.root.findAll((node) => String(node.type) === 'Switch' && node.props.accessibilityRole === 'switch'),
      ).toHaveLength(1)
      for (const key of ['rating', 'reaction', 'choice', 'details']) {
        expect(renderer.root.findByProps({ testID: `fidibeki-field-${key}` })).toBeTruthy()
      }
      expect(renderer.root.findByProps({ accessibilityLabel: 'Close' })).toBeTruthy()
      renderer.unmount()
    },
  )

  it('uses no modal transition when reduced motion is enabled', async () => {
    const native = (await import('react-native')) as unknown as NativeTestControls
    native.__setPlatform('ios')
    native.__setReducedMotion(true)
    const renderer = await renderSDK()
    await openSheet(renderer)
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.animationType).toBe('none')
    renderer.unmount()
  })

  it('shows localized validation errors before any network request', async () => {
    const renderer = await renderSDK()
    await openSheet(renderer)
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
    })
    const rendered = JSON.stringify(renderer.toJSON())
    expect(rendered.match(/This response is required/g)?.length).toBe(3)
    expect(globalThis.fetch).not.toHaveBeenCalled()
    renderer.unmount()
  })

  it('invokes the screenshot host only after the explicit action and handles cancellation', async () => {
    const screenshotProvider = vi.fn(async () => null)
    const renderer = await renderSDK({ screenshotProvider })
    expect(screenshotProvider).not.toHaveBeenCalled()
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, 'Add screenshot')
      await flush()
    })
    expect(screenshotProvider).toHaveBeenCalledOnce()
    expect(JSON.stringify(renderer.toJSON())).toContain('Screenshot selection cancelled')
    renderer.unmount()
  })

  it('surfaces a host permission/provider error without requesting native permission itself', async () => {
    const screenshotProvider = vi.fn(async () => {
      throw new Error('photo permission denied by host')
    })
    const renderer = await renderSDK({ screenshotProvider })
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, 'Add screenshot')
      await flush()
    })
    expect(JSON.stringify(renderer.toJSON())).toContain('The app could not provide a screenshot')
    renderer.unmount()
  })

  it('clears consent on locale/notice refresh while preserving compatible answers', async () => {
    const first = session()
    const second: MobileCaptureSession = {
      ...first,
      token: 'fmc_second_capture_session_token_123456',
      configuration: {
        ...first.configuration,
        display_locale: 'pt',
        contact_notice: {
          ...first.configuration.contact_notice!,
          notice_version: '2026-08-controller-v2',
          locale: 'pt',
        },
      },
    }
    const sessionProvider = vi.fn<SessionProvider>().mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    const requestBodies: string[] = []
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(async (_input, init) => {
        requestBodies.push(String(init?.body))
        return new Response(
          JSON.stringify({
            error: {
              code: 'configuration_changed',
              message: 'definition or notice changed',
            },
          }),
          { status: 409 },
        )
      })
      .mockImplementationOnce(async (_input, init) => {
        requestBodies.push(String(init?.body))
        return new Response(
          JSON.stringify({
            feedback_id: 'feedback-id',
            feedback_public_id: 'FDBK-42',
            attachments: [],
          }),
          { status: 200 },
        )
      })
    vi.stubGlobal('fetch', fetchMock)
    const renderer = await renderSDK({ sessionProvider })
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, '5 out of 5')
      pressByLabel(renderer.root, 'Alpha')
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Keep this answer')
      renderer.root.findByProps({ accessibilityLabel: 'Email for updates' }).props.onChangeText('reporter@example.test')
      renderer.root.findByProps({ accessibilityLabel: 'I agree to receive updates' }).props.onValueChange(true)
    })
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
      await flush()
    })
    expect(sessionProvider).toHaveBeenCalledTimes(2)
    expect(
      renderer.root.findByProps({
        accessibilityLabel: 'I agree to receive updates',
      }).props.value,
    ).toBe(false)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.value).toBe('Keep this answer')
    const submitted = JSON.parse(requestBodies[0]!) as Record<string, unknown>
    expect(submitted.notify_reporter).toBe(true)
    expect((submitted.consent as Record<string, unknown>).locale).toBe('en')
    renderer.unmount()
  })

  it('never submits hidden contact data after contact collection is disabled', async () => {
    const first = session()
    const second = structuredClone(first)
    second.token = 'fmc_contact_disabled_session_token_123456'
    second.configuration.definition.collect_contact = false
    delete second.configuration.contact_notice
    const sessionProvider = vi.fn<SessionProvider>().mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    const requestBodies: string[] = []
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(async (_input, init) => {
        requestBodies.push(String(init?.body))
        return new Response(
          JSON.stringify({
            error: {
              code: 'configuration_changed',
              message: 'definition changed',
            },
          }),
          { status: 409 },
        )
      })
      .mockImplementationOnce(async (_input, init) => {
        requestBodies.push(String(init?.body))
        return new Response(
          JSON.stringify({
            feedback_id: 'feedback-id',
            feedback_public_id: 'FDBK-42',
            attachments: [],
          }),
          { status: 200 },
        )
      })
    vi.stubGlobal('fetch', fetchMock)
    const renderer = await renderSDK({ sessionProvider })
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, '5 out of 5')
      pressByLabel(renderer.root, 'Alpha')
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Keep this answer')
      renderer.root.findByProps({ accessibilityLabel: 'Email for updates' }).props.onChangeText('reporter@example.test')
      renderer.root.findByProps({ accessibilityLabel: 'I agree to receive updates' }).props.onValueChange(true)
    })
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
      await flush()
    })
    expect(renderer.root.findAllByProps({ accessibilityLabel: 'Email for updates' })).toHaveLength(0)
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
    })
    const retried = JSON.parse(requestBodies[1]!) as Record<string, unknown>
    expect(retried).not.toHaveProperty('reporter_email')
    expect(retried).not.toHaveProperty('notify_reporter')
    expect(retried).not.toHaveProperty('consent')
    renderer.unmount()
  })

  it('acquires a fresh session and clears the draft after session expiry', async () => {
    const refreshed = session({
      token: 'fmc_refreshed_capture_session_token_123456',
    })
    const sessionProvider = vi.fn<SessionProvider>().mockResolvedValueOnce(session()).mockResolvedValueOnce(refreshed)
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: { code: 'session_expired', message: 'expired' },
            }),
            { status: 410 },
          ),
      ),
    )
    const renderer = await renderSDK({ sessionProvider })
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, '5 out of 5')
      pressByLabel(renderer.root, 'Alpha')
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Must not cross sessions')
    })
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
      await flush()
    })
    expect(sessionProvider).toHaveBeenCalledTimes(2)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.value).toBe('')
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(true)
    renderer.unmount()
  })

  it('preserves an open draft across equivalent host callback and storage identities', async () => {
    const firstProvider = vi.fn<SessionProvider>(async () => session())
    const replacementProvider = vi.fn<SessionProvider>(async () => session())
    const tree = (sessionProvider: SessionProvider, storage: StorageAdapter) => (
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        osVersion="18.5"
        screen="/checkout/review"
        sessionProvider={sessionProvider}
        storage={storage}
      >
        <HostFeedbackTrigger />
      </FidibekiProvider>
    )
    let renderer!: ReactTestRenderer
    await act(async () => {
      renderer = TestRenderer.create(tree(firstProvider, new MemoryStorage()))
      await flush()
    })
    await openSheet(renderer)
    await act(async () => {
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Keep this draft')
      renderer.update(tree(replacementProvider, new MemoryStorage()))
      await flush()
    })
    expect(firstProvider).toHaveBeenCalledOnce()
    expect(replacementProvider).not.toHaveBeenCalled()
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(true)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.value).toBe('Keep this draft')
    renderer.unmount()
  })

  it('preserves a draft when configuration refresh succeeds after a transient mint failure', async () => {
    const refreshed = session({
      token: 'fmc_refreshed_capture_session_token_123456',
    })
    const sessionProvider = vi
      .fn<SessionProvider>()
      .mockResolvedValueOnce(session())
      .mockRejectedValueOnce(new Error('session endpoint unavailable'))
      .mockResolvedValueOnce(refreshed)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: {
              code: 'configuration_changed',
              message: 'definition changed',
            },
          }),
          { status: 409 },
        ),
      ),
    )
    const renderer = await renderSDK({ sessionProvider })
    await openSheet(renderer)
    await fillRequiredForm(renderer)
    await act(async () => {
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText('Keep across refresh retry')
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
      await flush()
    })
    expect(sessionProvider).toHaveBeenCalledTimes(2)
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(false)
    await act(async () => {
      renderer.root.findByProps({ testID: 'host-feedback-trigger' }).props.onPress()
      await flush()
    })
    expect(sessionProvider).toHaveBeenCalledTimes(3)
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(true)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.value).toBe('Keep across refresh retry')
    renderer.unmount()
  })

  it('ignores an obsolete session request that resolves after the current screen', async () => {
    let resolveFirst!: (value: MobileCaptureSession) => void
    let resolveSecond!: (value: MobileCaptureSession) => void
    const firstPromise = new Promise<MobileCaptureSession>((resolve) => {
      resolveFirst = resolve
    })
    const secondPromise = new Promise<MobileCaptureSession>((resolve) => {
      resolveSecond = resolve
    })
    const sessionProvider = vi.fn<SessionProvider>((request) =>
      request.screen === '/first' ? firstPromise : secondPromise,
    )
    const tree = (screen: string) => (
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        osVersion="18.5"
        screen={screen}
        sessionProvider={sessionProvider}
        storage={new MemoryStorage()}
      >
        <HostFeedbackTrigger />
      </FidibekiProvider>
    )
    let renderer!: ReactTestRenderer
    await act(async () => {
      renderer = TestRenderer.create(tree('/first'))
      await Promise.resolve()
    })
    await act(async () => {
      renderer.update(tree('/second'))
      resolveSecond(
        session({
          configuration: {
            ...session().configuration,
            definition: definition({
              localization: {
                ...definition().localization,
                prompt: 'Current screen feedback',
              },
            }),
          },
        }),
      )
      await flush()
    })
    await act(async () => {
      resolveFirst(
        session({
          configuration: {
            ...session().configuration,
            definition: definition({
              localization: {
                ...definition().localization,
                prompt: 'Obsolete screen feedback',
              },
            }),
          },
        }),
      )
      await flush()
    })
    expect(JSON.stringify(renderer.toJSON())).toContain('Current screen feedback')
    expect(JSON.stringify(renderer.toJSON())).not.toContain('Obsolete screen feedback')
    renderer.unmount()
  })

  it('invalidates an open session when a screen refresh fails', async () => {
    const storage = new MemoryStorage()
    const sessionProvider = vi.fn<SessionProvider>(async (request) => {
      if (request.screen === '/second') throw new Error('session endpoint unavailable')
      return session()
    })
    const tree = (screen: string) => (
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        osVersion="18.5"
        screen={screen}
        sessionProvider={sessionProvider}
        storage={storage}
      >
        <HostFeedbackTrigger />
      </FidibekiProvider>
    )
    let renderer!: ReactTestRenderer
    await act(async () => {
      renderer = TestRenderer.create(tree('/first'))
      await flush()
    })
    await openSheet(renderer)
    await fillRequiredForm(renderer)
    const staleSubmit = renderer.root.findByProps({
      testID: 'fidibeki-submit',
    }).props.onPress
    await act(async () => {
      renderer.update(tree('/second'))
      await flush()
      await flush()
    })
    expect(renderer.root.findAllByProps({ testID: 'fidibeki-modal' })).toHaveLength(0)
    await act(async () => {
      staleSubmit()
      await flush()
    })
    expect(globalThis.fetch).not.toHaveBeenCalled()
    renderer.unmount()
  })

  it('closes a visible modal when refreshed recurrence is ineligible', async () => {
    const storage = new MemoryStorage()
    storage.values.set(
      recurrenceStorageKey('project-1', 'mobile-example', '/second'),
      JSON.stringify({ state: 'submitted', recordedAt: Date.now() }),
    )
    const ineligible = session({
      configuration: {
        ...session().configuration,
        definition: definition({ recurrence_mode: 'never' }),
      },
    })
    const sessionProvider = vi.fn<SessionProvider>(async (request) =>
      request.screen === '/second' ? ineligible : session(),
    )
    const tree = (screen: string) => (
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        osVersion="18.5"
        screen={screen}
        sessionProvider={sessionProvider}
        storage={storage}
      >
        <HostFeedbackTrigger />
      </FidibekiProvider>
    )
    let renderer!: ReactTestRenderer
    await act(async () => {
      renderer = TestRenderer.create(tree('/first'))
      await flush()
    })
    await openSheet(renderer)
    await fillRequiredForm(renderer)
    const staleSubmit = renderer.root.findByProps({
      testID: 'fidibeki-submit',
    }).props.onPress
    await act(async () => {
      renderer.update(tree('/second'))
      await flush()
    })
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(false)
    expect(renderer.root.findByProps({ testID: 'host-feedback-trigger' }).props.disabled).toBe(true)
    expect(renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.disabled).toBe(true)
    await act(async () => {
      staleSubmit()
      await flush()
    })
    expect(globalThis.fetch).not.toHaveBeenCalled()
    renderer.unmount()
  })

  it('reactivates a cooldown host trigger without requiring an app restart', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-28T12:00:00Z'))
    const current = session({
      configuration: {
        ...session().configuration,
        definition: definition({
          recurrence_mode: 'cooldown',
          recurrence_days: 1,
        }),
      },
    })
    const storage = new MemoryStorage()
    const key = recurrenceStorageKey('project-1', 'mobile-example', '/checkout/review')
    storage.values.set(
      key,
      JSON.stringify({
        state: 'submitted',
        recordedAt: Date.now() - 24 * 60 * 60 * 1000 + 1_000,
      }),
    )
    const sessionProvider = vi.fn(async () => current)
    let renderer!: ReactTestRenderer
    await act(async () => {
      renderer = TestRenderer.create(
        <FidibekiProvider
          apiBaseUrl="https://api.example.test"
          appVersion="1.2.3"
          osVersion="18.5"
          screen="/checkout/review"
          sessionProvider={sessionProvider}
          storage={storage}
        >
          <HostFeedbackTrigger />
        </FidibekiProvider>,
      )
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(renderer.root.findByProps({ testID: 'host-feedback-trigger' }).props.disabled).toBe(true)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(sessionProvider).toHaveBeenCalledTimes(2)
    expect(renderer.root.findByProps({ testID: 'host-feedback-trigger' }).props.disabled).toBe(false)
    renderer.unmount()
    vi.useRealTimers()
  })

  it('never renders an SDK-owned launcher for web presentation metadata', async () => {
    const edge = session({
      configuration: {
        ...session().configuration,
        definition: definition({
          launcher_variant: 'edge_tab',
          appearance: {
            position: 'bottom-left',
            accent_color: '#123456',
            accent_foreground: '#ffffff',
          },
        }),
      },
    })
    const renderer = await renderSDK({
      sessionProvider: vi.fn(async () => edge),
    })
    expect(renderer.root.findAllByProps({ testID: 'fidibeki-launcher' })).toHaveLength(0)
    expect(renderer.root.findByProps({ testID: 'host-feedback-trigger' })).toBeTruthy()
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.visible).toBe(false)
    renderer.unmount()
  })

  it('resolves automatic appearance from the native color scheme', async () => {
    const native = (await import('react-native')) as unknown as NativeTestControls
    native.__setColorScheme('dark')
    const automatic = session({
      configuration: {
        ...session().configuration,
        definition: definition({ appearance: { theme: 'auto' } }),
      },
    })
    const renderer = await renderSDK({
      sessionProvider: vi.fn(async () => automatic),
    })
    await openSheet(renderer)
    const safeArea = renderer.root.find((node) => String(node.type) === 'SafeAreaView')
    expect(Object.assign({}, ...safeArea.props.style).backgroundColor).toBe('#141716')
    renderer.unmount()
  })

  it('falls back labels for valid legacy backend definitions', async () => {
    const legacy = structuredClone(session())
    delete legacy.configuration.definition.localization.block_labels
    delete legacy.configuration.definition.localization.option_labels
    legacy.configuration.definition.response_blocks = [
      {
        key: 'legacy_choice',
        type: 'single_choice',
        required: false,
        options: [{ value: 'very_happy' }, { value: 'not_happy' }],
      },
    ]
    const renderer = await renderSDK({
      sessionProvider: vi.fn(async () => legacy),
    })
    await openSheet(renderer)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Feedback' })).toBeTruthy()
    expect(renderer.root.findByProps({ accessibilityLabel: 'Very Happy' })).toBeTruthy()
    renderer.unmount()
  })

  it('omits the native AI disclosure for web-only audio/video categories', async () => {
    const webOnly = structuredClone(session())
    webOnly.configuration.definition.ai_processing!.input_categories = ['audio', 'video']
    const renderer = await renderSDK({
      sessionProvider: vi.fn(async () => webOnly),
    })
    await openSheet(renderer)
    expect(JSON.stringify(renderer.toJSON())).not.toContain('Submitted text and screenshots may be processed')
    renderer.unmount()
  })
})
