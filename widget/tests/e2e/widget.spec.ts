import { expect, test } from '@playwright/test'
import type { WidgetConfiguration, WidgetRule } from '../../src/types'

const browserRule: WidgetRule = {
  id: 'rule-browser', name: 'Checkout', priority: 10,
  matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/sample',
  trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
  can_rate: true, can_write_text: true, can_record_audio: true, can_record_video: true,
  can_attach_element: true, can_attach_screenshot: false,
  greeting: 'How clear was checkout?', position: 'bottom-right', theme: 'light',
  accent_color: '#147d64', replay_mode: 'sampling', replay_sampling_rate: 100,
  replay_max_duration_seconds: 1800, replay_max_events: 50000,
  replay_mask_all_inputs: true, replay_mask_text_selector: null,
  recurrence_mode: 'cooldown', recurrence_days: 30,
}

function browserConfiguration(rule: WidgetRule = browserRule): WidgetConfiguration {
  return {
    project_id: 'project-browser',
    refresh_after_seconds: 300,
    error_reporting_dsn: 'https://public@example.invalid/1',
    features: {
      ai: false, session_replay: false, slack: false, video: false,
      audio: false, dom_capture: true, custom_domain: false,
    },
    rules: [rule],
  }
}

test('loads the matching rule and serializes feedback through the public contract', async ({ page }) => {
  const loadedScripts: string[] = []
  const submissions: Array<Record<string, unknown>> = []
  const connections: string[] = []
  let feedbackAttempts = 0
  page.on('request', (request) => {
    if (request.resourceType() === 'script') loadedScripts.push(request.url())
  })
  await page.route('**/v1/widget/config*', async (route) => {
    expect(route.request().headers()['x-fidibeki-public-key']).toBe('pk_live_browser_contract')
    expect(new URL(route.request().url()).searchParams.has('page_url')).toBe(false)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(browserConfiguration()),
    })
  })
  await page.route('**/v1/widget/connected*', async (route) => {
		const requestURL = new URL(route.request().url())
		expect(requestURL.searchParams.get('key')).toBe('pk_live_browser_contract')
		expect(requestURL.searchParams.get('rule_id')).toBe('rule-browser')
		connections.push(requestURL.href)
		await route.fulfill({ status: 204 })
	})
  await page.route('**/v1/feedback?*', async (route) => {
		expect(new URL(route.request().url()).searchParams.get('key')).toBe('pk_live_browser_contract')
    submissions.push(route.request().postDataJSON() as Record<string, unknown>)
    feedbackAttempts++
    await route.fulfill({
      status: feedbackAttempts === 1 ? 500 : 201,
      contentType: 'application/json', body: feedbackAttempts === 1 ? '{}' : '{"feedback_id":"fb_test"}',
    })
  })
  await page.route('**/api/1/envelope/**', (route) => route.fulfill({ status: 200, body: '' }))

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await expect.poll(() => connections.length).toBeGreaterThan(0)
  if (process.env.FIDIBEKI_VISUAL === '1') {
    await page.waitForTimeout(300)
    await page.screenshot({ path: '/tmp/fidibeki-widget-desktop.png', fullPage: true })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: '/tmp/fidibeki-widget-mobile.png', fullPage: true })
    await page.setViewportSize({ width: 1280, height: 720 })
  }
  await page.getByRole('button', { name: '5 out of 5' }).click()
  await page.getByRole('textbox', { name: 'Feedback' }).fill('The delivery date was easy to understand.')

  await page.getByRole('button', { name: 'Attach page element' }).click()
  await page.locator('#checkout-summary').hover()
  await page.locator('#checkout-summary').click({ position: { x: 20, y: 20 } })
  await expect(page.getByRole('button', { name: 'Element attached' })).toBeVisible()
  expect(loadedScripts.some((url) => url.endsWith('/v1/error-reporting.js'))).toBe(false)
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Could not send feedback. Please try again.')).toBeVisible()
  await expect.poll(() => loadedScripts.some((url) => url.endsWith('/v1/error-reporting.js'))).toBe(true)
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()

  const submission = submissions[0]
	expect(submissions[1]?.client_feedback_id).toBe(submission?.client_feedback_id)
  expect(submission).toMatchObject({
    widget_rule_id: 'rule-browser',
    rating: 5,
    text: 'The delivery date was easy to understand.',
    company_website: '',
    metadata: {
      url: 'http://127.0.0.1:4173/sample.html',
      viewport_width: 1280,
      viewport_height: 720,
    },
    element_attachment: { selector: '#checkout-summary' },
  })
  expect(String((submission?.element_attachment as { html?: string }).html)).toContain('Checkout summary')
  expect(loadedScripts.some((url) => url.endsWith('/v1/widget.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/rating.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/element-attachment.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/audio.js'))).toBe(false)
  expect(loadedScripts.some((url) => url.endsWith('/v1/video.js'))).toBe(false)
  expect(loadedScripts.some((url) => url.endsWith('/v1/replay.js'))).toBe(false)

  await page.goto('/sample.html?asset=BIC')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
  expect(submissions).toHaveLength(2)
})

test('opening the widget does not suppress it after a refresh', async ({ page }) => {
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration()),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html?asset=BAI')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
})

test('a cooldown submission becomes eligible again at its expiry boundary', async ({ page }) => {
  const baseTime = Date.parse('2026-07-22T12:00:00.000Z')
  const clientFeedbackIDs: string[] = []
  await page.addInitScript((initialTime) => {
    Date.now = () => {
      try {
        return Number(localStorage.getItem('__fidibeki_test_time')) || initialTime
      } catch {
        return initialTime
      }
    }
  }, baseTime)
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration()),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
  await page.route('**/v1/feedback?*', async (route) => {
    const body = route.request().postDataJSON() as { client_feedback_id: string }
    clientFeedbackIDs.push(body.client_feedback_id)
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: '{"feedback_id":"fb_cooldown"}',
    })
  })

  await page.goto('/sample.html?asset=BAI')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await page.evaluate((expiry) => localStorage.setItem('__fidibeki_test_time', String(expiry)), baseTime + 30 * 86_400_000)
  await page.goto('/sample.html?asset=BIC')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  expect(clientFeedbackIDs).toHaveLength(2)
  expect(clientFeedbackIDs[1]).not.toBe(clientFeedbackIDs[0])
})

test('session recurrence suppresses reloads but permits a new top-level session', async ({ context, page }) => {
  const sessionRule = { ...browserRule, recurrence_mode: 'session', recurrence_days: null } satisfies WidgetRule
  await context.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration(sessionRule)),
  }))
  await context.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
  await context.route('**/v1/feedback?*', (route) => route.fulfill({
    status: 201,
    contentType: 'application/json',
    body: '{"feedback_id":"fb_session"}',
  }))

  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)

  const nextSession = await context.newPage()
  await nextSession.goto('/sample.html')
  await expect(nextSession.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
})

test('dismissal suppresses only the current top-level session', async ({ context, page }) => {
  await context.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration()),
  }))
  await context.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Close feedback' }).click()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)

  const nextSession = await context.newPage()
  await nextSession.goto('/sample.html')
  await expect(nextSession.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
})

test('never recurrence persists across sessions while another pathname remains eligible', async ({ context, page }) => {
  const neverRule = {
    ...browserRule,
    matcher_type: 'all_pages',
    matcher_value: null,
    recurrence_mode: 'never',
    recurrence_days: null,
  } satisfies WidgetRule
  await context.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration(neverRule)),
  }))
  await context.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
  await context.route('**/v1/feedback?*', (route) => route.fulfill({
    status: 201,
    contentType: 'application/json',
    body: '{"feedback_id":"fb_never"}',
  }))

  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()

  const nextSession = await context.newPage()
  await nextSession.goto('/sample.html')
  await expect(nextSession.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
  await nextSession.evaluate(() => history.pushState({}, '', '/another-page'))
  await expect(nextSession.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
})

test('keeps an accepted feedback successful when its attachment upload fails', async ({ page }) => {
  let submissions = 0
  const attachmentRule = { ...browserRule, can_attach_element: false, can_attach_screenshot: true } satisfies WidgetRule
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration(attachmentRule)),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
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
  await page.route('**/v1/feedback?*', async (route) => {
    submissions++
    const body = route.request().postDataJSON() as { attachments: Array<{ client_attachment_id: string }> }
    expect(body.attachments).toHaveLength(1)
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        feedback_id: 'fb_attachment',
        attachments: [{
          attachment_id: 'attachment-one',
          client_attachment_id: body.attachments[0]!.client_attachment_id,
          presigned_url: 'http://127.0.0.1:4173/storage/upload',
          method: 'PUT', headers: {}, complete_token: 'complete-one',
          expires_at: '2026-07-22T13:00:00Z',
        }],
      }),
    })
  })
  await page.route('**/storage/upload', (route) => route.fulfill({ status: 403, body: '' }))
  await page.route('**/api/1/envelope/**', (route) => route.fulfill({ status: 200, body: '' }))

  await page.goto('/sample.html?asset=BAI')
  await page.getByRole('button', { name: 'Capture screenshot' }).click()
  await expect(page.getByRole('button', { name: 'Screenshot attached' })).toBeVisible()
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Feedback received.')).toBeVisible()
  await expect(page.getByText('One or more attachments could not be uploaded.')).toBeVisible()
  await expect(page.getByText('Could not send feedback. Please try again.')).toHaveCount(0)
  expect(submissions).toBe(1)

  await page.goto('/sample.html?asset=BIC')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
})
