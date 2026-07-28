import React from 'react'
import TestRenderer, {
  act,
  type ReactTestInstance,
  type ReactTestRenderer,
} from 'react-test-renderer'
import {
  FidibekiFeedbackLauncher,
  FidibekiProvider,
  type MobileCaptureSession,
  type ScreenshotProvider,
  type SessionProvider,
} from '../src'
import { MemoryStorage, session } from './fixtures'

interface NativeTestControls {
  Platform: { OS: 'ios' | 'android' }
  __setPlatform(platform: 'ios' | 'android'): void
  __setReducedMotion(enabled: boolean): void
}

async function flush(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve))
}

async function renderSDK(options: {
  sessionProvider?: SessionProvider
  screenshotProvider?: ScreenshotProvider
} = {}): Promise<ReactTestRenderer> {
  const provider = options.sessionProvider ?? vi.fn(async () => session())
  let renderer!: ReactTestRenderer
  await act(async () => {
    renderer = TestRenderer.create(
      <FidibekiProvider
        apiBaseUrl="https://api.example.test"
        appVersion="1.2.3"
        locale="en"
        osVersion="18.5"
        screen="/checkout/review?private=true"
        sessionProvider={provider}
        storage={new MemoryStorage()}
        {...(options.screenshotProvider
          ? { screenshotProvider: options.screenshotProvider }
          : {})}
      >
        <FidibekiFeedbackLauncher />
      </FidibekiProvider>,
    )
    await flush()
  })
  return renderer
}

async function openSheet(renderer: ReactTestRenderer): Promise<void> {
  const launcher = renderer.root.findByProps({ testID: 'fidibeki-launcher' })
  await act(async () => {
    launcher.props.onPress()
    await flush()
  })
}

function pressByLabel(root: ReactTestInstance, label: string): void {
  root.findByProps({ accessibilityLabel: label }).props.onPress()
}

describe('native provider, launcher, and modal sheet', () => {
  beforeEach(async () => {
    const native = (await import('react-native')) as unknown as NativeTestControls
    native.__setReducedMotion(false)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
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
      expect(
        renderer.root.findByProps({ testID: `fidibeki-keyboard-${platform}` }).props.behavior,
      ).toBe(platform === 'ios' ? 'padding' : 'height')
      expect(
        renderer.root.findAll(
          (node) => String(node.type) === 'SafeAreaView',
        ),
      ).toHaveLength(1)
      expect(
        renderer.root.findAll(
          (node) =>
            String(node.type) === 'View' && node.props.accessibilityRole === 'radiogroup',
        ),
      ).toHaveLength(3)
      expect(
        renderer.root.findAll(
          (node) =>
            String(node.type) === 'Switch' && node.props.accessibilityRole === 'switch',
        ),
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
    expect(renderer.root.findByProps({ testID: 'fidibeki-modal' }).props.animationType).toBe(
      'none',
    )
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
    expect(JSON.stringify(renderer.toJSON())).toContain(
      'The app could not provide a screenshot',
    )
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
    const sessionProvider = vi
      .fn<SessionProvider>()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
    const requestBodies: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
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
      }),
    )
    const renderer = await renderSDK({ sessionProvider })
    await openSheet(renderer)
    await act(async () => {
      pressByLabel(renderer.root, '5 out of 5')
      pressByLabel(renderer.root, 'Alpha')
      renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.onChangeText(
        'Keep this answer',
      )
      renderer.root
        .findByProps({ accessibilityLabel: 'Email for updates' })
        .props.onChangeText('reporter@example.test')
      renderer.root
        .findByProps({ accessibilityLabel: 'I agree to receive updates' })
        .props.onValueChange(true)
    })
    await act(async () => {
      renderer.root.findByProps({ testID: 'fidibeki-submit' }).props.onPress()
      await flush()
      await flush()
    })
    expect(sessionProvider).toHaveBeenCalledTimes(2)
    expect(
      renderer.root.findByProps({ accessibilityLabel: 'I agree to receive updates' }).props
        .value,
    ).toBe(false)
    expect(renderer.root.findByProps({ accessibilityLabel: 'Details' }).props.value).toBe(
      'Keep this answer',
    )
    const submitted = JSON.parse(requestBodies[0]!) as Record<string, unknown>
    expect(submitted.notify_reporter).toBe(true)
    expect((submitted.consent as Record<string, unknown>).locale).toBe('en')
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
    expect(JSON.stringify(renderer.toJSON())).not.toContain(
      'Submitted text and screenshots may be processed',
    )
    renderer.unmount()
  })
})
