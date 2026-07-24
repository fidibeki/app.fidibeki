import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import type { WidgetConfiguration, WidgetRule } from '../../src/types'

const attachmentRule: WidgetRule = {
  id: 'rule-attachments', name: 'Attachments', priority: 10,
  matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/sample',
  trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
  can_rate: false, can_write_text: true, can_record_audio: true, can_record_video: true,
  can_attach_element: false, can_attach_screenshot: true,
  greeting: 'Tell us more', position: 'bottom-right', theme: 'light',
  accent_color: '#147d64', replay_mode: 'none', replay_sampling_rate: 0,
  replay_max_duration_seconds: 1800, replay_max_events: 50000,
  replay_mask_all_inputs: true, replay_mask_text_selector: null,
  replay_record_canvas: false, replay_canvas_fps: 2,
  replay_consent: {
    mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
    supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
  },
  analytics_consent: {
    mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
    supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
  },
  recurrence_mode: 'cooldown', recurrence_days: 30,
  localizations: {
    en: {
      launcher_label: 'Feedback', prompt: 'Tell us more',
      comment_placeholder: 'Add context', contact_label: 'Email address',
      consent_label: 'Email me updates about this feedback', submit_label: 'Send feedback',
      success_message: 'Your feedback was sent.',
    },
  },
}

function attachmentConfiguration(): WidgetConfiguration {
  return {
    project_id: 'project-attachments',
    refresh_after_seconds: 300,
    features: {
      ai: false, session_replay: false, slack: false, video: true,
      audio: true, dom_capture: true, custom_domain: false,
    },
    rules: [attachmentRule],
  }
}

// Installs a deterministic MediaRecorder + getUserMedia stand-in so audio/video recording is
// reproducible in CI (no real microphone/camera), while still running the widget's actual built
// audio.js/video.js modules end to end in a real browser engine.
async function installFakeMediaRecordingEnvironment(
  page: Page,
  supportedAudioTypes: string[],
  supportedVideoTypes: string[],
): Promise<void> {
  await page.addInitScript(({ supportedAudioTypes, supportedVideoTypes }) => {
    class FakeMediaStreamTrack {
      stop(): void {}
    }
    class FakeMediaStream {
      getTracks(): FakeMediaStreamTrack[] {
        return [new FakeMediaStreamTrack()]
      }
    }
    class FakeMediaRecorder extends EventTarget {
      static isTypeSupported(type: string): boolean {
        const list = type.startsWith('audio/') ? supportedAudioTypes : supportedVideoTypes
        return list.includes(type)
      }

      mimeType: string
      state: 'inactive' | 'recording' = 'inactive'

      constructor(_stream: unknown, options?: { mimeType?: string }) {
        super()
        this.mimeType = options?.mimeType ?? ''
      }

      start(): void {
        this.state = 'recording'
      }

      stop(): void {
        this.state = 'inactive'
        const chunk = new Event('dataavailable') as Event & { data?: Blob }
        chunk.data = new Blob(['fixture-bytes'], { type: this.mimeType })
        this.dispatchEvent(chunk)
        this.dispatchEvent(new Event('stop'))
      }
    }
    Object.defineProperty(window, 'MediaRecorder', { configurable: true, value: FakeMediaRecorder })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => new FakeMediaStream() },
    })
  }, { supportedAudioTypes, supportedVideoTypes })
}

async function mockScreenshotModule(page: Page): Promise<void> {
  await page.route('**/v1/screenshot.js', (route) => route.fulfill({
    status: 200,
    contentType: 'text/javascript',
    body: `export function createScreenshotControl() {
      const element = document.createElement('button')
      element.type = 'button'
      element.className = 'secondary'
      element.textContent = 'Capture screenshot'
      let screenshot = null
      element.addEventListener('click', () => {
        screenshot = new Blob(['image'], { type: 'image/png' })
        element.textContent = 'Screenshot attached'
      })
      return { element, screenshot: () => screenshot }
    }`,
  }))
}

test.beforeEach(async ({ page }) => {
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(attachmentConfiguration()),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
})

// Audio, video, and screenshot each mount via an independent dynamic import, so they can settle
// in any order and cannot be told apart by DOM position. Each step below is driven by the
// control's current, unique button text instead - and since audio is fully recorded (started and
// stopped) before video starts, "Stop recording" never matches more than one button at a time.
async function recordThroughUI(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Record audio', exact: true }).click()
  await page.getByRole('button', { name: 'Stop recording', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Audio attached', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Record video', exact: true }).click()
  await page.getByRole('button', { name: 'Stop recording', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Video attached', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Capture screenshot', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Screenshot attached', exact: true })).toBeVisible()
}

test('declares every simultaneously captured attachment kind with a browser-appropriate MIME type', async ({ page }, testInfo) => {
  const isFirefox = testInfo.project.name === 'firefox'
  await installFakeMediaRecordingEnvironment(
    page,
    isFirefox ? ['audio/ogg;codecs=opus', 'audio/ogg'] : ['audio/webm;codecs=opus', 'audio/webm'],
    ['video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm'],
  )
  await mockScreenshotModule(page)
  const submissions: Array<Record<string, unknown>> = []
  await page.route('**/v1/feedback?*', async (route) => {
    submissions.push(route.request().postDataJSON() as Record<string, unknown>)
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        feedback_id: 'fb_attachments',
        attachments: [],
        warnings: [{
          code: 'service_unavailable', reason: 'upload_rate_limited',
          message: 'Feedback saved without attachments. Try again later.', retry_after_seconds: 5,
        }],
      }),
    })
  })

  await page.goto('/sample.html')
  await recordThroughUI(page)

  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await expect(page.getByText('Feedback saved without attachments. Try again later.')).toBeVisible()

  expect(submissions).toHaveLength(1)
  const attachments = submissions[0]?.attachments as Array<{ kind: string; content_type: string }>
  // None of the three simultaneously attached kinds may be silently omitted from the declaration.
  expect(attachments).toHaveLength(3)
  const byKind = Object.fromEntries(attachments.map(attachment => [attachment.kind, attachment.content_type]))
  expect(byKind.screenshot).toBe('image/png')
  // The declared content_type strips codec parameters (the backend allowlist only recognizes
  // base MIME types), but it must still reflect the actually negotiated container per browser.
  expect(byKind.video).toBe('video/webm')
  expect(byKind.audio).toBe(isFirefox ? 'audio/ogg' : 'audio/webm')
})

test('uploads and completes every simultaneously captured attachment kind', async ({ page }, testInfo) => {
  const isFirefox = testInfo.project.name === 'firefox'
  await installFakeMediaRecordingEnvironment(
    page,
    isFirefox ? ['audio/ogg;codecs=opus', 'audio/ogg'] : ['audio/webm;codecs=opus', 'audio/webm'],
    ['video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm'],
  )
  await mockScreenshotModule(page)

  let declared: Array<{ client_attachment_id: string; kind: string; content_type: string }> = []
  const uploaded: string[] = []
  const completed: string[] = []
  await page.route('**/v1/feedback?*', async (route) => {
    const payload = route.request().postDataJSON() as { attachments: typeof declared }
    declared = payload.attachments
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        feedback_id: 'fb_attachments_ok',
        attachments: payload.attachments.map((attachment, index) => ({
          attachment_id: `attachment-${index}`,
          client_attachment_id: attachment.client_attachment_id,
          presigned_url: `http://127.0.0.1:4173/storage/upload-${index}`,
          method: 'PUT', headers: {}, complete_token: `complete-${index}`,
          expires_at: '2026-07-22T13:00:00Z',
        })),
      }),
    })
  })
  await page.route('**/storage/upload-*', (route) => {
    uploaded.push(route.request().url())
    return route.fulfill({ status: 200, body: '' })
  })
  await page.route('**/v1/attachments/*/complete*', (route) => {
    completed.push(route.request().url())
    return route.fulfill({ status: 204 })
  })

  await page.goto('/sample.html')
  await recordThroughUI(page)
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()

  // Every declared attachment reaches both the storage upload and the completion call - none is
  // left declared-but-unuploaded or uploaded-but-never-completed.
  await expect.poll(() => uploaded.length).toBe(3)
  await expect.poll(() => completed.length).toBe(3)
  expect(declared).toHaveLength(3)
  const byKind = Object.fromEntries(declared.map(attachment => [attachment.kind, attachment.content_type]))
  expect(byKind.screenshot).toBe('image/png')
  expect(byKind.video).toBe('video/webm')
  expect(byKind.audio).toBe(isFirefox ? 'audio/ogg' : 'audio/webm')
})

test('completes unaffected attachments and attributes a completion network failure', async ({ page }, testInfo) => {
  const isFirefox = testInfo.project.name === 'firefox'
  await installFakeMediaRecordingEnvironment(
    page,
    isFirefox ? ['audio/ogg;codecs=opus', 'audio/ogg'] : ['audio/webm;codecs=opus', 'audio/webm'],
    ['video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm'],
  )
  await mockScreenshotModule(page)
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ ...attachmentConfiguration(), error_reporting_dsn: 'https://errors.example.test/1' }),
  }))
  await page.route('**/v1/error-reporting.js', (route) => route.fulfill({
    status: 200,
    contentType: 'text/javascript',
    body: `export function captureWidgetError(error) {
      window.__attachmentErrors = window.__attachmentErrors || []
      window.__attachmentErrors.push({ name: error?.name, message: error?.message })
    }`,
  }))
  await page.route('**/v1/widget/events*', (route) => route.fulfill({ status: 204 }))

  let declared: Array<{ client_attachment_id: string; kind: string }> = []
  const uploaded: string[] = []
  const completed: string[] = []
  await page.route('**/v1/feedback?*', async (route) => {
    const payload = route.request().postDataJSON() as { attachments: typeof declared }
    declared = payload.attachments
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        feedback_id: 'fb_attachments_partial',
        attachments: payload.attachments.map((attachment, index) => ({
          attachment_id: `attachment-${index}`,
          client_attachment_id: attachment.client_attachment_id,
          presigned_url: `http://127.0.0.1:4173/storage/upload-${index}`,
          method: 'PUT', headers: {}, complete_token: `complete-${index}`,
          expires_at: '2026-07-22T13:00:00Z',
        })),
      }),
    })
  })
  await page.route('**/storage/upload-*', (route) => {
    uploaded.push(route.request().url())
    return route.fulfill({ status: 200, body: '' })
  })
  await page.route('**/v1/attachments/*/complete*', (route) => {
    const attachmentID = route.request().url().match(/attachments\/([^/]+)\/complete/)?.[1] ?? ''
    if (attachmentID === 'attachment-1') return route.abort('failed')
    completed.push(attachmentID)
    return route.fulfill({ status: 204 })
  })

  await page.goto('/sample.html')
  await recordThroughUI(page)
  await page.getByRole('button', { name: 'Send feedback' }).click()

  await expect.poll(() => uploaded.length).toBe(3)
  await expect.poll(() => completed.sort()).toEqual(['attachment-0', 'attachment-2'])
  expect(declared.map(attachment => attachment.kind)).toEqual(['audio', 'video', 'screenshot'])
  await expect.poll(() => page.evaluate(() => {
    const errors = (window as unknown as {
      __attachmentErrors?: Array<{ name?: string, message?: string }>
    }).__attachmentErrors ?? []
    return errors.find(error => error.name === 'AttachmentPipelineError')
  })).toEqual({
      name: 'AttachmentPipelineError',
      message: 'attachment pipeline failed for 1 of the declared attachment(s): video/complete',
    })
})
