import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import type { WidgetConfiguration, WidgetPresentation, WidgetRule } from '../../src/types'

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

function definitionConfiguration(presentation: WidgetPresentation, key = 'inline-checkout'): WidgetConfiguration {
  return {
    ...browserConfiguration(),
    schema_version: 2,
    default_locale: 'en',
    rules: [],
    definitions: [{
      ...browserRule,
      id: `definition-${presentation}`,
      definition_key: key,
      definition_version: 2,
      definition_status: 'active',
      presentation,
      objective: 'continuous_feedback',
      response_blocks: [
        { key: 'score', type: 'rating', required: true },
        { key: 'reaction', type: 'reaction', required: true, options: [{ value: 'helpful' }, { value: 'not_helpful' }] },
        { key: 'reason', type: 'single_choice', required: false, options: [{ value: 'fast' }, { value: 'clear' }] },
        { key: 'details', type: 'free_text', required: true },
      ],
      localizations: {
        en: {
          launcher_label: 'Share feedback', prompt: 'How was checkout?',
          comment_placeholder: 'Add context', contact_label: 'Email address',
          consent_label: 'Email me about this feedback', submit_label: 'Send response',
          success_message: 'Response received.',
          block_labels: { score: 'Score', reaction: 'Was this helpful?', reason: 'Reason', details: 'Details' },
          option_labels: {
            reaction: { helpful: 'Helpful', not_helpful: 'Not helpful' },
            reason: { fast: 'Fast', clear: 'Clear' },
          },
        },
      },
      allow_comment: true,
      collect_contact: true,
    }],
  }
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const nativeSetTimeout = window.setTimeout.bind(window)
    window.setTimeout = ((handler: TimerHandler, timeout?: number, ...args: unknown[]) =>
      nativeSetTimeout(handler, timeout === 5_000 ? 0 : timeout, ...args)) as typeof window.setTimeout
  })
  await page.route('**/v1/widget/events*', (route) => {
    const payload = route.request().postDataJSON() as { events: Array<{ event_id: string }> }
    return route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ accepted_event_ids: payload.events.map(event => event.event_id), duplicate_event_ids: [] }),
    })
  })
})

test('emits matched once per canonical page view before suppression', async ({ page }) => {
  const events: Array<Record<string, string>> = []
  await page.unroute('**/v1/widget/events*')
  await page.route('**/v1/widget/events*', async (route) => {
    const payload = route.request().postDataJSON() as { events: Array<Record<string, string>> }
    events.push(...payload.events)
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ accepted_event_ids: payload.events.map(event => event.event_id), duplicate_event_ids: [] }),
    })
  })
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration()),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html?asset=BAI#price')
  await expect.poll(() => events.filter(event => event.event_type === 'matched').length).toBe(1)
  await page.evaluate(() => history.replaceState({}, '', '/sample.html/?asset=BIC#details'))
  await expect.poll(() => events.filter(event => event.event_type === 'matched').length).toBe(1)
  await page.evaluate(() => {
    history.pushState({}, '', '/not-targeted?asset=BFA')
    history.pushState({}, '', '/sample.html?asset=ENSA')
  })
  await expect.poll(() => events.filter(event => event.event_type === 'matched').length).toBe(2)

  const matchedEvents = events.filter(event => event.event_type === 'matched')
  expect(matchedEvents.map(event => event.page_url)).toEqual([
    'http://127.0.0.1:4173/sample.html',
    'http://127.0.0.1:4173/sample.html',
  ])
  expect(matchedEvents[1]?.visitor_id).toBe(matchedEvents[0]?.visitor_id)
  expect(matchedEvents[1]?.page_view_id).not.toBe(matchedEvents[0]?.page_view_id)
  expect(matchedEvents[0]?.rule_id).toBe('rule-browser')
})

test('records visibility and only the first meaningful interaction', async ({ page }) => {
  const eventTypes: string[] = []
  await page.unroute('**/v1/widget/events*')
  await page.route('**/v1/widget/events*', async (route) => {
    const payload = route.request().postDataJSON() as { events: Array<{ event_id: string; event_type: string }> }
    eventTypes.push(...payload.events.map(event => event.event_type))
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ accepted_event_ids: payload.events.map(event => event.event_id), duplicate_event_ids: [] }),
    })
  })
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration()),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await expect.poll(() => eventTypes).toEqual(['matched', 'impression'])
  await page.getByRole('textbox', { name: 'Feedback' }).focus()
  await page.getByRole('heading', { name: 'How clear was checkout?' }).hover()
  await page.waitForTimeout(100)
  expect(eventTypes).toEqual(['matched', 'impression'])

  await page.getByRole('button', { name: '5 out of 5' }).click()
  await expect.poll(() => eventTypes).toEqual(['matched', 'impression', 'interaction'])
  await page.getByRole('textbox', { name: 'Feedback' }).fill('Still only one interaction.')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await page.waitForTimeout(100)
  expect(eventTypes).toEqual(['matched', 'impression', 'interaction'])
})

const meaningfulInteractions: Array<{ name: string; act(page: Page): Promise<void> }> = [
  {
    name: 'rating selection',
    act: async (page) => page.getByRole('button', { name: '4 out of 5' }).click(),
  },
  {
    name: 'feedback text',
    act: async (page) => page.getByRole('textbox', { name: 'Feedback' }).fill('A concrete observation.'),
  },
  {
    name: 'attachment start',
    act: async (page) => {
      await page.getByRole('button', { name: 'Attach page element' }).click()
      await page.keyboard.press('Escape')
    },
  },
  {
    name: 'submission attempt',
    act: async (page) => page.getByRole('button', { name: 'Send feedback' }).click(),
  },
]

for (const interaction of meaningfulInteractions) {
  test(`counts ${interaction.name} as a meaningful interaction`, async ({ page }) => {
    const events = await captureAnalytics(page, browserConfiguration())
    await page.route('**/v1/feedback?*', (route) => route.fulfill({ status: 500, body: '{}' }))
    await page.goto('/sample.html')
    await expect.poll(() => events).toEqual(['matched', 'impression'])
    await interaction.act(page)
    await expect.poll(() => events).toEqual(['matched', 'impression', 'interaction'])
  })
}

for (const trigger of ['scroll', 'exit_intent'] as const) {
  test(`${trigger} records impression only after the trigger fires`, async ({ page }) => {
    if (trigger === 'scroll') {
      await page.addInitScript(() => {
        window.addEventListener('DOMContentLoaded', () => {
          document.body.style.minHeight = '4000px'
        })
      })
    }
    const configuredRule = {
      ...browserRule,
      trigger_type: trigger,
      trigger_delay_seconds: null,
      trigger_scroll_percent: trigger === 'scroll' ? 50 : null,
    } satisfies WidgetRule
    const events = await captureAnalytics(page, browserConfiguration(configuredRule))
    await page.goto('/sample.html')
    await expect.poll(() => events).toEqual(['matched'])
    await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
    if (trigger === 'scroll') {
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    } else {
      await page.evaluate(() => document.dispatchEvent(new MouseEvent('mouseout', {
        clientY: 0,
        relatedTarget: null,
      })))
    }
    await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
    await expect.poll(() => events).toEqual(['matched', 'impression'])
  })
}

test('does not count an untriggered or recurrence-suppressed widget as visible', async ({ page }) => {
  const events: string[] = []
  const delayedRule = { ...browserRule, trigger_delay_seconds: 60 } satisfies WidgetRule
  let configuration = browserConfiguration(delayedRule)
  await page.unroute('**/v1/widget/events*')
  await page.route('**/v1/widget/events*', async (route) => {
    const payload = route.request().postDataJSON() as { events: Array<{ event_id: string; event_type: string }> }
    events.push(...payload.events.map(event => event.event_type))
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ accepted_event_ids: payload.events.map(event => event.event_id), duplicate_event_ids: [] }),
    })
  })
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
  await page.route('**/v1/feedback?*', (route) => route.fulfill({
    status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_suppressed"}',
  }))

  await page.goto('/sample.html')
  await expect.poll(() => events).toEqual(['matched'])
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)

  configuration = browserConfiguration({ ...browserRule, recurrence_mode: 'never', recurrence_days: null })
  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
  expect(events.filter(event => event === 'matched')).toHaveLength(3)
  expect(events.filter(event => event === 'impression')).toHaveLength(1)
})

async function captureAnalytics(page: Page, configuration: WidgetConfiguration): Promise<string[]> {
  const events: string[] = []
  await page.unroute('**/v1/widget/events*')
  await page.route('**/v1/widget/events*', async (route) => {
    const payload = route.request().postDataJSON() as { events: Array<{ event_id: string; event_type: string }> }
    events.push(...payload.events.map(event => event.event_type))
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ accepted_event_ids: payload.events.map(event => event.event_id), duplicate_event_ids: [] }),
    })
  })
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))
  return events
}

for (const presentation of ['launcher', 'inline', 'reaction', 'prompt'] as const) {
  test(`${presentation} definition collects structured feedback through declarative placement`, async ({ page }) => {
    const submissions: Array<Record<string, unknown>> = []
    await page.route('**/v1/widget/config*', route => route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify(definitionConfiguration(presentation)),
    }))
    await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
    await page.route('**/v1/feedback?*', route => {
      submissions.push(route.request().postDataJSON() as Record<string, unknown>)
      return route.fulfill({
        status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_structured"}',
      })
    })

    await page.goto('/sample.html')
    if (presentation === 'launcher') {
      await expect(page.getByRole('heading', { name: 'How was checkout?' })).toHaveCount(0)
      await page.getByRole('button', { name: 'Share feedback' }).click()
    }
    await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
    if (presentation === 'inline') {
      await expect(page.locator('fidibeki-widget[rule="inline-checkout"] > div[id^="fidibeki-widget-host-"]')).toHaveCount(1)
    }
    await page.getByRole('button', { name: '4 out of 5' }).click()
    await page.getByRole('button', { name: 'Helpful', exact: true }).click()
    await page.getByRole('combobox', { name: 'Reason' }).selectOption('fast')
    await page.getByRole('textbox', { name: 'Details' }).fill('The checkout was clear.')
    await page.getByRole('textbox', { name: 'Add context' }).fill('No blockers.')
    await page.getByRole('textbox', { name: 'Email address' }).fill('customer@example.com')
    await page.getByRole('checkbox', { name: 'Email me about this feedback' }).check()
    await page.getByRole('button', { name: 'Send response' }).click()
    await expect(page.getByText('Response received.')).toBeVisible()

    expect(submissions).toHaveLength(1)
    expect(submissions[0]).toMatchObject({
      widget_rule_id: `definition-${presentation}`,
      rating: 4,
      text: 'The checkout was clear.',
      responses: { score: 4, reaction: 'helpful', reason: 'fast', details: 'The checkout was clear.' },
      comment: 'No blockers.',
      reporter_email: 'customer@example.com',
      notify_reporter: true,
    })
  })
}

test('imperative mount resolves a definition key and safely rejects inactive definitions', async ({ page }) => {
  const active = definitionConfiguration('inline', 'imperative-checkout').definitions![0]!
  const inactive = { ...active, id: 'definition-paused', definition_key: 'paused-checkout', definition_status: 'paused' as const }
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ ...definitionConfiguration('inline', 'imperative-checkout'), definitions: [active, inactive] }),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))

  await page.goto('/sample.html')
  const results = await page.evaluate(async () => {
    const api = (window as unknown as {
      Fidibeki: { mount(element: HTMLElement, options: { rule: string }): Promise<{ mounted: boolean; reason?: string }> }
    }).Fidibeki
    const target = document.querySelector<HTMLElement>('#imperative-widget')!
    const activeResult = await api.mount(target, { rule: 'imperative-checkout' })
    const pausedResult = await api.mount(document.createElement('div'), { rule: 'paused-checkout' })
    return {
      active: { mounted: activeResult.mounted, reason: activeResult.reason },
      paused: { mounted: pausedResult.mounted, reason: pausedResult.reason },
    }
  })
  expect(results.active.mounted).toBe(true)
  expect(results.paused).toMatchObject({ mounted: false, reason: 'inactive' })
  await expect(page.locator('#imperative-widget > div[id^="fidibeki-widget-host-"]')).toHaveCount(1)
  await expect(page.locator('#imperative-widget').getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
})

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
	expect(submissions[1]?.analytics).toEqual(submission?.analytics)
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
		analytics: {
			event_id: expect.any(String),
			visitor_id: expect.any(String),
			page_view_id: expect.any(String),
			rule_id: 'rule-browser',
			page_url: 'http://127.0.0.1:4173/sample.html',
			occurred_at: expect.any(String),
		},
  })
  expect((submission?.analytics as Record<string, unknown>).event_type).toBeUndefined()
  expect(String((submission?.element_attachment as { html?: string }).html)).toContain('Checkout summary')
  expect(loadedScripts.some((url) => url.endsWith('/v1/widget.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/rating.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/element-attachment.js'))).toBe(true)
  expect(loadedScripts.some((url) => url.endsWith('/v1/audio.js'))).toBe(false)
  expect(loadedScripts.some((url) => url.endsWith('/v1/video.js'))).toBe(false)
  expect(loadedScripts.some((url) => url.endsWith('/v1/replay.js'))).toBe(false)
  expect(loadedScripts.some((url) => url.endsWith('/v1/targeting-regex.js'))).toBe(false)

  await page.goto('/sample.html?asset=BIC')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
  expect(submissions).toHaveLength(2)
})

test('dismissal records explicit close without manufacturing interaction', async ({ page }) => {
  const events = await captureAnalytics(page, browserConfiguration())
  await page.goto('/sample.html')
  await expect.poll(() => events).toEqual(['matched', 'impression'])
  await page.getByRole('button', { name: 'Close feedback' }).click()
  await expect.poll(() => events).toEqual(['matched', 'impression', 'dismissed'])
})

test('failed submission can be dismissed but accepted submission cannot', async ({ page }) => {
  const events = await captureAnalytics(page, browserConfiguration())
  let attempts = 0
  let resolveAttempt: ((status: number) => void) | undefined
  const releaseResponse = (status: number) => {
    const resolve = resolveAttempt
    if (!resolve) throw new Error('feedback request is not pending')
    resolveAttempt = undefined
    resolve(status)
  }
  await page.route('**/v1/feedback?*', async (route) => {
    attempts++
    const status = await new Promise<number>((resolve) => {
      resolveAttempt = resolve
    })
    return route.fulfill({
      status,
      contentType: 'application/json',
      body: status === 500 ? '{}' : '{"feedback_id":"fb_accepted"}',
    })
  })

  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect.poll(() => resolveAttempt).toBeDefined()
  const failedClose = page.getByRole('button', { name: 'Close feedback' })
  await expect(failedClose).toBeDisabled()
  await failedClose.dispatchEvent('click')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  expect(events).not.toContain('dismissed')
  releaseResponse(500)
  await expect(page.getByText('Could not send feedback. Please try again.')).toBeVisible()
  await expect(failedClose).toBeEnabled()
  await failedClose.click()
  await expect.poll(() => events).toEqual(['matched', 'impression', 'interaction', 'dismissed'])

  await page.evaluate(() => sessionStorage.clear())
  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect.poll(() => resolveAttempt).toBeDefined()
  const acceptedClose = page.getByRole('button', { name: 'Close feedback' })
  await expect(acceptedClose).toBeDisabled()
  await acceptedClose.dispatchEvent('click')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  releaseResponse(201)
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await expect(acceptedClose).toBeDisabled()
  await acceptedClose.dispatchEvent('click')
  await page.waitForTimeout(100)
  expect(attempts).toBe(2)
  expect(events.filter(event => event === 'dismissed')).toHaveLength(1)
  expect(events).not.toContain('submitted')
})

test('uses additive targeting conditions and normalized page URLs', async ({ page }) => {
  const configuredRule = {
    ...browserRule,
    matcher_type: 'url_exact',
    matcher_value: 'http://127.0.0.1:4173/sample.html',
    include_conditions: [
      { matcher_type: 'url_exact', matcher_value: 'http://127.0.0.1:4173/sample.html', position: 0 },
      { matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/docs', position: 1 },
    ],
    exclude_conditions: [
      { matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/sample.html/private', position: 0 },
    ],
  } satisfies WidgetRule
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration(configuredRule)),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html?asset=BAI#price')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.evaluate(() => history.pushState({}, '', '/sample.html/?asset=BIC#details'))
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.evaluate(() => history.pushState({}, '', '/sample.html/private?asset=BIC#details'))
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toHaveCount(0)
  await page.evaluate(() => history.pushState({}, '', '/docs/getting-started/?source=nav'))
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
})

test('loads the safe regex asset only for a regex rule', async ({ page }) => {
  const loadedScripts: string[] = []
  page.on('request', (request) => {
    if (request.resourceType() === 'script') loadedScripts.push(request.url())
  })
  const regexRule = {
    ...browserRule,
    include_conditions: [{
      matcher_type: 'url_regex',
      matcher_value: '^http://127\\.0\\.0\\.1:4173/sample\\.html$',
      position: 0,
    }],
    exclude_conditions: [],
  } satisfies WidgetRule
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(browserConfiguration(regexRule)),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html?asset=BAI#price')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  expect(loadedScripts.some((url) => url.endsWith('/v1/targeting-regex.js'))).toBe(true)
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
