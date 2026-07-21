import { expect, test } from '@playwright/test'

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
      body: JSON.stringify({
        project_id: 'project-browser',
        refresh_after_seconds: 300,
        error_reporting_dsn: 'https://public@example.invalid/1',
        features: {
          ai: false, session_replay: false, slack: false, video: false,
          audio: false, dom_capture: true, custom_domain: false,
        },
        rules: [{
          id: 'rule-browser', name: 'Checkout', priority: 10,
          matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/sample',
          trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
          can_rate: true, can_write_text: true, can_record_audio: true, can_record_video: true,
          can_attach_element: true, can_attach_screenshot: false,
          greeting: 'How clear was checkout?', position: 'bottom-right', theme: 'light',
          accent_color: '#147d64', replay_mode: 'sampling', replay_sampling_rate: 100,
          replay_max_duration_seconds: 1800, replay_max_events: 50000,
          replay_mask_all_inputs: true, replay_mask_text_selector: null,
        }],
      }),
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
})
