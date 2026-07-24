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
  replay_record_canvas: false, replay_canvas_fps: 2,
  replay_consent: {
    mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
    supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
  },
  recurrence_mode: 'cooldown', recurrence_days: 30,
  localizations: {
    en: {
      launcher_label: 'Feedback', prompt: 'How clear was checkout?',
      comment_placeholder: 'Tell us more', contact_label: 'Email address',
      consent_label: 'Email me updates about this feedback', submit_label: 'Send feedback',
      success_message: 'Your feedback was sent.',
    },
    pt: {
      launcher_label: 'Feedback', prompt: 'O checkout foi claro?',
      comment_placeholder: 'Conta-nos mais', contact_label: 'Endereço de email',
      consent_label: 'Enviem-me atualizações sobre este feedback', submit_label: 'Enviar feedback',
      success_message: 'O feedback foi enviado.',
    },
  },
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

function customerReplayRule(rule: WidgetRule): WidgetRule {
  return {
    ...rule,
    replay_consent: {
      mode: 'customer_cmp',
      notice_version: '2026-07',
      controller_name: '',
      privacy_policy_url: '',
      supported_locales: ['en'],
      retention_days: 0,
      expiry_days: 30,
      notices: {},
    },
  }
}

function managedReplayRule(rule: WidgetRule): WidgetRule {
  return {
    ...rule,
    replay_consent: {
      mode: 'fidibeki_managed',
      notice_version: '2026-07',
      controller_name: 'Acme Studio',
      privacy_policy_url: 'https://example.com/privacy',
      supported_locales: ['en'],
      retention_days: 30,
      expiry_days: 30,
      notices: {
        en: {
          title: 'Help us understand this experience',
          description: 'Allow Acme Studio to record anonymous interactions on this page.',
          accept_label: 'Accept',
          reject_label: 'Reject',
          withdraw_label: 'Withdraw consent',
        },
      },
    },
  }
}

async function seedActiveReplayConsent(
  page: Page,
  source: 'customer_cmp' | 'fidibeki_managed' = 'customer_cmp',
) {
  await page.addInitScript((configuredSource) => {
    localStorage.setItem(
      'fidibeki:replay-consent:v1:project-browser:rule-browser:2026-07',
      JSON.stringify({
        version: 1,
        state: 'active',
        token: 'existing-consent-token',
        locale: 'en',
        noticeVersion: '2026-07',
        source: configuredSource,
        ...(configuredSource === 'customer_cmp' ? { consentId: 'existing-cmp-reference' } : {}),
        expiresAt: '2099-01-01T00:00:00Z',
      }),
    )
  }, source)
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
          consent_label: 'Email me updates about this feedback', submit_label: 'Send response',
          success_message: 'Response received.',
          block_labels: { score: 'Score', reaction: 'Was this helpful?', reason: 'Reason', details: 'Details' },
          option_labels: {
            reaction: { helpful: 'Helpful', not_helpful: 'Not helpful' },
            reason: { fast: 'Fast', clear: 'Clear' },
          },
        },
        pt: {
          launcher_label: 'Dar feedback', prompt: 'Como foi o checkout?',
          comment_placeholder: 'Adicionar contexto', contact_label: 'Endereço de email',
          consent_label: 'Enviem-me atualizações sobre este feedback', submit_label: 'Enviar resposta',
          success_message: 'Resposta recebida.',
          block_labels: { score: 'Pontuação', reaction: 'Foi útil?', reason: 'Motivo', details: 'Detalhes' },
          option_labels: {
            reaction: { helpful: 'Útil', not_helpful: 'Não foi útil' },
            reason: { fast: 'Rápido', clear: 'Claro' },
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
    'http://127.0.0.1:4173/sample',
    'http://127.0.0.1:4173/sample',
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

test('shows the governed AI disclosure and customer privacy link before collection', async ({ page }) => {
  const aiRule: WidgetRule = {
    ...browserRule,
    ai_processing: {
      enabled: true,
      provider: 'google_gemini',
      input_categories: ['feedback_text', 'audio'],
      privacy_policy_url: 'https://customer.example/privacy',
      policy_version: '2026-07-24',
    },
  }
  const configuration = browserConfiguration(aiRule)
  configuration.features.ai = true
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', (route) => route.fulfill({ status: 204 }))

  await page.goto('/sample.html')
  const disclosure = page.getByText(
    'Selected feedback content may be processed by Google Gemini for advisory analysis. Content types: written feedback, audio recordings.',
  )
  await expect(disclosure).toBeVisible()
  await expect(page.getByRole('link', { name: 'Privacy policy' })).toHaveAttribute(
    'href',
    'https://customer.example/privacy',
  )
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
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
    await page.evaluate(() => { document.documentElement.lang = 'pt-AO' })
    await expect(page.getByRole('heading', { name: 'Como foi o checkout?' })).toBeVisible()
    await page.evaluate(() => { document.documentElement.lang = 'en' })
    await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
    if (presentation === 'inline') {
      await expect(page.locator('fidibeki-widget[rule="inline-checkout"] > div[id^="fidibeki-widget-host-"]')).toHaveCount(1)
    }
    await page.getByRole('button', { name: '4 out of 5' }).click()
    await page.getByRole('button', { name: 'Helpful', exact: true }).click()
    await page.getByRole('combobox', { name: 'Reason' }).selectOption('fast')
    await page.getByRole('textbox', { name: 'Details' }).fill('The checkout was clear.')
    await page.getByRole('textbox', { name: 'Add context' }).fill('No blockers.')
    const email = page.getByRole('textbox', { name: 'Email address' })
    await expect(email).toBeHidden()
    await page.getByRole('checkbox', { name: 'Email me updates about this feedback' }).check()
    await expect(email).toBeVisible()
    await email.fill('customer@example.com')
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
      consent: {
        purpose: 'feedback_updates',
        notice_version: expect.any(String),
        locale: expect.any(String),
        granted: true,
      },
    })
  })
}

test('uses Portuguese page language before English browser preferences', async ({ page }) => {
  const configuration = definitionConfiguration('inline')
  const submissions: Array<Record<string, unknown>> = []
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/feedback?*', route => {
    submissions.push(route.request().postDataJSON() as Record<string, unknown>)
    return route.fulfill({
      status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_portuguese"}',
    })
  })
  await page.route('**/sample.html', async (route) => {
    const response = await route.fetch()
    const body = (await response.text()).replace('<html lang="en">', '<html lang="pt">')
    await route.fulfill({ response, body })
  })
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'languages', { configurable: true, value: ['en-US'] })
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'en-US' })
  })

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'Como foi o checkout?' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Enviar resposta' })).toBeVisible()
  await page.getByRole('button', { name: 'Enviar resposta' }).click()
  await expect(page.getByText('Preenche a resposta obrigatória.')).toBeVisible()

  await page.getByRole('button', { name: '4 de 5' }).click()
  await page.getByRole('button', { name: 'Útil', exact: true }).click()
  await page.getByRole('textbox', { name: 'Detalhes' }).fill('A experiência foi clara.')
  await page.getByRole('button', { name: 'Enviar resposta' }).click()
  await expect(page.getByText('Resposta recebida.')).toBeVisible()

  expect(submissions[0]?.metadata).toMatchObject({ language: 'pt' })
})

test('reactive page language preserves an in-progress response and funnel state', async ({ page }) => {
  const events = await captureAnalytics(page, definitionConfiguration('inline'))
  const submissions: Array<Record<string, unknown>> = []
  await page.route('**/v1/feedback?*', route => {
    submissions.push(route.request().postDataJSON() as Record<string, unknown>)
    return route.fulfill({
      status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_reactive_locale"}',
    })
  })

  await page.goto('/sample.html')
  await expect.poll(() => events).toEqual(['matched', 'impression'])
  await page.getByRole('button', { name: '4 out of 5' }).click()
  await page.getByRole('button', { name: 'Helpful', exact: true }).click()
  await page.getByRole('combobox', { name: 'Reason' }).selectOption('fast')
  const details = page.locator('textarea[name="response_details"]')
  const email = page.locator('input[name="reporter_email"]')
  const consent = page.locator('input[name="notify_reporter"]')
  await details.fill('Keep this draft across the language change.')
  await consent.check()
  await email.fill('invalid-email')
  await page.getByRole('button', { name: 'Send response' }).click()
  await expect(page.getByText('Enter a valid email address.')).toBeVisible()
  await expect.poll(() => events).toEqual(['matched', 'impression', 'interaction'])

  await page.evaluate(() => { document.documentElement.lang = 'pt-AO' })

  await expect(page.getByRole('heading', { name: 'Como foi o checkout?' })).toBeVisible()
  await expect(page.getByText('Introduz um endereço de email válido.')).toBeVisible()
  await expect(details).toHaveValue('Keep this draft across the language change.')
  await expect(email).toHaveValue('invalid-email')
  await expect(email).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByRole('button', { name: '4 de 5' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: 'Útil', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('combobox', { name: 'Motivo' })).toHaveValue('fast')
  expect(events).toEqual(['matched', 'impression', 'interaction'])

  await email.fill('customer@example.com')
  await page.getByRole('button', { name: 'Enviar resposta' }).click()
  await expect(page.getByText('Resposta recebida.')).toBeVisible()
  expect(submissions[0]?.metadata).toMatchObject({ language: 'pt' })
})

test('automatic placement reacts to the page language without duplicate funnel events', async ({ page }) => {
  const events = await captureAnalytics(page, browserConfiguration())

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await expect.poll(() => events).toEqual(['matched', 'impression'])

  await page.evaluate(() => { document.documentElement.lang = 'pt-AO' })

  await expect(page.getByRole('heading', { name: 'O checkout foi claro?' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Fechar feedback' })).toBeVisible()
  expect(events).toEqual(['matched', 'impression'])
})

const contactSubmissionCases = [
  { name: 'disabled', enabled: true, disabled: true, check: false as const, email: '' },
  { name: 'unchecked', enabled: true, disabled: false, check: false as const, email: '' },
  {
    name: 'checked then unchecked clears the typed email',
    enabled: true, disabled: false, check: 'toggle' as const, email: 'customer@example.com',
  },
  {
    name: 'checked with a valid email',
    enabled: true, disabled: false, check: true as const, email: 'customer@example.com',
  },
]

for (const contactCase of contactSubmissionCases) {
  test(`contact collection submits the ${contactCase.name} state`, async ({ page }) => {
    const configuration = definitionConfiguration('inline')
    configuration.definitions![0]!.collect_contact = !contactCase.disabled
    const submissions: Array<Record<string, unknown>> = []
    await page.route('**/v1/widget/config*', route => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
    }))
    await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
    await page.route('**/v1/feedback?*', route => {
      submissions.push(route.request().postDataJSON() as Record<string, unknown>)
      return route.fulfill({ status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_contact"}' })
    })

    await page.goto('/sample.html')
    const email = page.getByRole('textbox', { name: 'Email address' })
    const consent = page.getByRole('checkbox', { name: 'Email me updates about this feedback' })
    if (contactCase.disabled) {
      await expect(email).toHaveCount(0)
      await expect(consent).toHaveCount(0)
    } else {
      // The email input is present but must stay out of the accessible/visible surface until
      // the reporter opts in, per the reveal-only-after-consent contract.
      await expect(email).toBeHidden()
      if (contactCase.check === true || contactCase.check === 'toggle') {
        await consent.check()
        await expect(email).toBeVisible()
        if (contactCase.email) await email.fill(contactCase.email)
      }
      if (contactCase.check === 'toggle') {
        await consent.uncheck()
        await expect(email).toBeHidden()
        // getByRole excludes hidden elements from the accessibility tree entirely, so read the
        // cleared value through a raw locator instead.
        await expect(page.locator('input[name="reporter_email"]')).toHaveValue('')
      }
    }
    await page.getByRole('button', { name: '4 out of 5' }).click()
    await page.getByRole('button', { name: 'Helpful', exact: true }).click()
    await page.getByRole('textbox', { name: 'Details' }).fill('Contact preference regression.')
    await page.getByRole('button', { name: 'Send response' }).click()
    await expect(page.getByText('Response received.')).toBeVisible()

    const expectGranted = contactCase.check === true
    expect(submissions).toHaveLength(1)
    expect(submissions[0]).toMatchObject({
      reporter_email: expectGranted ? contactCase.email : null,
      notify_reporter: expectGranted,
      consent: expectGranted ? {
        purpose: 'feedback_updates', notice_version: expect.any(String),
        locale: expect.any(String), granted: true,
      } : null,
    })
  })
}

test('contact validation is inline and preserves the prepared response', async ({ page }) => {
  const submissions: Array<Record<string, unknown>> = []
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(definitionConfiguration('inline')),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/feedback?*', route => {
    submissions.push(route.request().postDataJSON() as Record<string, unknown>)
    return route.fulfill({ status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_contact"}' })
  })

  await page.goto('/sample.html')
  await page.getByRole('button', { name: '4 out of 5' }).click()
  await page.getByRole('button', { name: 'Helpful', exact: true }).click()
  const details = page.getByRole('textbox', { name: 'Details' })
  const email = page.getByRole('textbox', { name: 'Email address' })
  const consent = page.getByRole('checkbox', { name: 'Email me updates about this feedback' })
  await details.fill('Keep this response while correcting contact details.')
  await expect(email).toBeHidden()
  await consent.check()
  await expect(email).toBeVisible()
  await email.fill('not-an-email')
  await page.getByRole('button', { name: 'Send response' }).click()

  await expect(page.getByText('Enter a valid email address.')).toBeVisible()
  await expect(email).toHaveValue('not-an-email')
  await expect(email).toHaveAttribute('aria-invalid', 'true')
  await expect(details).toHaveValue('Keep this response while correcting contact details.')
  await expect(page.getByRole('button', { name: '4 out of 5' })).toHaveAttribute('aria-pressed', 'true')
  expect(submissions).toHaveLength(0)

  await email.fill('')
  await page.getByRole('button', { name: 'Send response' }).click()
  await expect(page.getByText('Enter an email address to receive updates.')).toBeVisible()
  await expect(details).toHaveValue('Keep this response while correcting contact details.')
  expect(submissions).toHaveLength(0)
})

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

test('automatic reaction waits for the configured objective policy', async ({ page }) => {
  const configuration = definitionConfiguration('reaction', 'policy-reaction')
  configuration.definitions![0]!.objective = 'after_engagement'
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'How was checkout?' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Place order' }).click()
  await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
})

test('applies controlled appearance tokens and a privacy-safe logo', async ({ page }) => {
  const configuration = definitionConfiguration('inline')
  configuration.definitions![0]!.appearance = {
    position: 'bottom-left', theme: 'dark', accent_color: '#46c754',
    accent_foreground: '#171a19',
    surface_color: '#101817', text_color: '#f7faf9',
    logo_url: 'https://cdn.example.com/fidibeki-mark.svg',
  }
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
  await page.route('https://cdn.example.com/fidibeki-mark.svg', route => route.fulfill({
    status: 200, contentType: 'image/svg+xml',
    body: '<svg xmlns="http://www.w3.org/2000/svg" width="120" height="24"><rect width="120" height="24" fill="#005f56"/></svg>',
  }))

  await page.goto('/sample.html')
  const widget = page.locator('fidibeki-widget[rule="inline-checkout"]')
  const logo = widget.locator('img.brand')
  await expect(logo).toHaveAttribute('src', 'https://cdn.example.com/fidibeki-mark.svg')
  await expect(logo).toHaveAttribute('referrerpolicy', 'no-referrer')
  await expect(widget.locator('.panel')).toHaveCSS('background-color', 'rgb(16, 24, 23)')
  await expect(widget.locator('.panel')).toHaveCSS('color', 'rgb(247, 250, 249)')
  await expect(widget.getByRole('button', { name: 'Send response' })).toHaveCSS('background-color', 'rgb(70, 199, 84)')
  await expect(widget.getByRole('button', { name: 'Send response' })).toHaveCSS('color', 'rgb(23, 26, 25)')
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
      url: 'http://127.0.0.1:4173/sample',
      viewport_width: 1280,
      viewport_height: 720,
    },
    element_attachment: {
      selector: 'main > article',
      html: '<article><h1></h1><p></p></article>',
    },
    analytics: {
      event_id: expect.any(String),
      visitor_id: expect.any(String),
      page_view_id: expect.any(String),
      rule_id: 'rule-browser',
      page_url: 'http://127.0.0.1:4173/sample',
      occurred_at: expect.any(String),
    },
  })
  expect((submission?.analytics as Record<string, unknown>).event_type).toBeUndefined()
  expect(JSON.stringify(submission?.element_attachment)).not.toContain('checkout-summary')
  expect(JSON.stringify(submission?.element_attachment)).not.toContain('Checkout summary')
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

test('customer-managed replay stays dormant until a structured grant and stops on revocation', async ({ page }) => {
  const replayRule: WidgetRule = {
    ...browserRule,
    replay_mode: 'full',
    replay_sampling_rate: 0,
    replay_consent: {
      mode: 'customer_cmp',
      notice_version: '2026-07',
      controller_name: '',
      privacy_policy_url: '',
      supported_locales: ['en'],
      retention_days: 0,
      expiry_days: 30,
      notices: {},
    },
  }
  const baseConfiguration = browserConfiguration(replayRule)
  const configuration: WidgetConfiguration = {
    ...baseConfiguration,
    features: { ...baseConfiguration.features, session_replay: true },
  }
  const replayScripts: string[] = []
  const sessionRequests: Array<Record<string, unknown>> = []
  let revocations = 0
  page.on('request', (request) => {
    if (request.url().endsWith('/v1/replay.js')) replayScripts.push(request.url())
  })
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/replay/**', async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname.endsWith('/replay/consents/revoke')) {
      revocations++
      return route.fulfill({ status: 204 })
    }
    if (pathname.endsWith('/replay/consents')) {
      const body = request.postDataJSON() as { grant_token: string }
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          token: body.grant_token,
          state: 'active',
          expires_at: '2026-08-23T12:00:00Z',
        }),
      })
    }
    if (pathname.endsWith('/replay/sessions')) {
      sessionRequests.push(request.postDataJSON() as Record<string, unknown>)
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'server-session',
          rrweb_version: '2.1.1',
          max_duration_seconds: 1800,
          max_events: 50000,
        }),
      })
    }
    if (pathname.endsWith('/batches')) {
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          presigned_url: 'http://127.0.0.1:4173/replay-upload',
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          complete_token: 'complete-token',
        }),
      })
    }
    if (pathname.endsWith('/complete')) return route.fulfill({ status: 204 })
    if (pathname.endsWith('/finalize')) {
      return route.fulfill({ status: 202, contentType: 'application/json', body: '{}' })
    }
    return route.abort()
  })
  await page.route('**/replay-upload', route => route.fulfill({ status: 200 }))

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await page.waitForTimeout(100)
  expect(replayScripts).toHaveLength(0)
  expect(sessionRequests).toHaveLength(0)

  await page.evaluate(async () => {
    const api = (window as typeof window & {
      Fidibeki: {
        replay: {
          grantConsent(assertion: {
            consentId: string
            purpose: 'session_replay'
            noticeVersion: string
            locale: string
            source: 'customer_cmp'
          }): Promise<unknown>
        }
      }
    }).Fidibeki
    await api.replay.grantConsent({
      consentId: 'cmp-reference',
      purpose: 'session_replay',
      noticeVersion: '2026-07',
      locale: 'en',
      source: 'customer_cmp',
    })
  })
  await expect.poll(() => sessionRequests.length).toBe(1)
  expect(replayScripts).toHaveLength(1)
  expect(sessionRequests[0]).toMatchObject({
    widget_rule_id: replayRule.id,
    consent_token: expect.any(String),
  })

  await page.evaluate(async () => {
    const api = (window as typeof window & {
      Fidibeki: { replay: { revokeConsent(): Promise<void> } }
    }).Fidibeki
    await api.replay.revokeConsent()
  })
  await expect.poll(() => revocations).toBe(1)
  await page.evaluate(() => window.dispatchEvent(new Event('fidibeki:navigation')))
  await page.waitForTimeout(100)
  expect(sessionRequests).toHaveLength(1)
})

test('revocation discards a replay capture that finishes starting after withdrawal', async ({ page }) => {
  const replayRule = customerReplayRule({
    ...browserRule,
    replay_mode: 'full',
    replay_sampling_rate: 0,
  })
  const baseConfiguration = browserConfiguration(replayRule)
  const configuration: WidgetConfiguration = {
    ...baseConfiguration,
    features: { ...baseConfiguration.features, session_replay: true },
  }
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/replay/consents/revoke*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/replay/consents*', async (route) => {
    const request = route.request().postDataJSON() as { grant_token: string }
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        token: request.grant_token,
        state: 'active',
        expires_at: '2026-08-23T12:00:00Z',
      }),
    })
  })
  await page.route('**/v1/replay.js', route => route.fulfill({
    status: 200,
    contentType: 'application/javascript',
    body: `
      export async function startReplayCapture() {
        window.__replayStartCount = (window.__replayStartCount || 0) + 1
        await new Promise(resolve => { window.__releaseReplayCapture = resolve })
        return {
          sessionID: 'delayed-session',
          stop: async () => { window.__replayStopCount = (window.__replayStopCount || 0) + 1 },
          revoke: () => { window.__replayRevokeCount = (window.__replayRevokeCount || 0) + 1 },
        }
      }
    `,
  }))

  await page.goto('/sample.html')
  await page.evaluate(async () => {
    const api = (window as typeof window & {
      Fidibeki: {
        replay: {
          grantConsent(assertion: {
            consentId: string
            purpose: 'session_replay'
            noticeVersion: string
            locale: string
            source: 'customer_cmp'
          }): Promise<unknown>
        }
      }
    }).Fidibeki
    await api.replay.grantConsent({
      consentId: 'cmp-reference',
      purpose: 'session_replay',
      noticeVersion: '2026-07',
      locale: 'en',
      source: 'customer_cmp',
    })
  })
  await expect.poll(() => page.evaluate(() =>
    (window as typeof window & { __replayStartCount?: number }).__replayStartCount ?? 0,
  )).toBe(1)

  await page.evaluate(async () => {
    const api = (window as typeof window & {
      Fidibeki: { replay: { revokeConsent(): Promise<void> } }
    }).Fidibeki
    await api.replay.revokeConsent()
    ;(window as typeof window & { __releaseReplayCapture?: () => void }).__releaseReplayCapture?.()
  })

  await expect.poll(() => page.evaluate(() => {
    const state = window as typeof window & {
      __replayStopCount?: number
      __replayRevokeCount?: number
    }
    return (state.__replayStopCount ?? 0) + (state.__replayRevokeCount ?? 0)
  })).toBe(1)
  expect(await page.evaluate(() =>
    (window as typeof window & { __replayRevokeCount?: number }).__replayRevokeCount ?? 0,
  )).toBe(1)
  expect(await page.evaluate(() =>
    (window as typeof window & { __replayStopCount?: number }).__replayStopCount ?? 0,
  )).toBe(0)
})

test('managed replay consent supports reject, accept, persistence, version changes, and withdrawal', async ({ page }) => {
  const replayRule: WidgetRule = {
    ...browserRule,
    replay_mode: 'full',
    replay_sampling_rate: 0,
    replay_consent: {
      mode: 'fidibeki_managed',
      notice_version: '2026-07',
      controller_name: 'Acme Studio',
      privacy_policy_url: 'https://example.com/privacy',
      supported_locales: ['en'],
      retention_days: 30,
      expiry_days: 30,
      notices: {
        en: {
          title: 'Help us understand this experience',
          description: 'Allow Acme Studio to record anonymous interactions on this page.',
          accept_label: 'Accept',
          reject_label: 'Reject',
          withdraw_label: 'Withdraw consent',
        },
      },
    },
  }
  const baseConfiguration = browserConfiguration(replayRule)
  let configuration: WidgetConfiguration = {
    ...baseConfiguration,
    default_locale: 'en',
    features: { ...baseConfiguration.features, session_replay: true },
  }
  const sessionRequests: Array<Record<string, unknown>> = []
  let revocations = 0
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/replay/**', async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname.endsWith('/replay/consents/revoke')) {
      revocations++
      return route.fulfill({ status: 204 })
    }
    if (pathname.endsWith('/replay/consents')) {
      const body = request.postDataJSON() as { state: string, grant_token: string }
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          state: body.state === 'denied' ? 'denied' : 'active',
          ...(body.state === 'granted' ? { token: body.grant_token } : {}),
          expires_at: '2026-08-23T12:00:00Z',
        }),
      })
    }
    if (pathname.endsWith('/replay/sessions')) {
      sessionRequests.push(request.postDataJSON() as Record<string, unknown>)
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'managed-session',
          rrweb_version: '2.1.1',
          max_duration_seconds: 1800,
          max_events: 50000,
        }),
      })
    }
    return route.abort()
  })

  await page.goto('/sample.html')
  await expect(page.getByRole('heading', { name: 'Help us understand this experience' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reject', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: 'Accept', exact: true })).toBeFocused()
  expect(sessionRequests).toHaveLength(0)
  await page.getByRole('button', { name: 'Reject', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Help us understand this experience' })).toHaveCount(0)

  await page.reload()
  await expect(page.getByRole('heading', { name: 'How clear was checkout?' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Help us understand this experience' })).toHaveCount(0)
  expect(sessionRequests).toHaveLength(0)

  await page.evaluate(() => {
    for (let index = localStorage.length - 1; index >= 0; index--) {
      const key = localStorage.key(index)
      if (key?.startsWith('fidibeki:replay-consent:')) localStorage.removeItem(key)
    }
    window.dispatchEvent(new Event('fidibeki:navigation'))
  })
  await expect(page.getByRole('heading', { name: 'Help us understand this experience' })).toBeVisible()
  await page.getByRole('button', { name: 'Accept', exact: true }).click()
  await expect.poll(() => sessionRequests.length).toBe(1)
  await expect(page.getByRole('button', { name: 'Replay privacy' })).toBeVisible()

  configuration = {
    ...configuration,
    rules: [{
      ...replayRule,
      replay_consent: {
        ...replayRule.replay_consent,
        notice_version: '2026-08',
        notices: {
          en: {
            ...replayRule.replay_consent.notices.en!,
            title: 'Updated replay permission',
          },
        },
      },
    }],
  }
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Updated replay permission' })).toBeVisible()
  expect(sessionRequests).toHaveLength(1)
  await page.getByRole('button', { name: 'Accept', exact: true }).click()
  await expect.poll(() => sessionRequests.length).toBe(2)
  await expect(page.getByRole('button', { name: 'Replay privacy' })).toBeVisible()

  await page.getByRole('button', { name: 'Replay privacy' }).click()
  await page.getByRole('button', { name: 'Withdraw consent' }).click()
  await expect.poll(() => revocations).toBe(1)
  await expect(page.getByRole('button', { name: 'Replay privacy' })).toHaveCount(0)
})

test('managed replay consent renders the supported Portuguese notice', async ({ page }) => {
  const replayRule: WidgetRule = {
    ...browserRule,
    replay_mode: 'full',
    replay_sampling_rate: 0,
    replay_consent: {
      mode: 'fidibeki_managed',
      notice_version: '2026-07',
      controller_name: 'Acme Studio',
      privacy_policy_url: 'https://example.com/privacidade',
      supported_locales: ['pt-pt'],
      retention_days: 30,
      expiry_days: 30,
      notices: {
        'pt-pt': {
          title: 'Ajude-nos a melhorar esta experiência',
          description: 'Autorize a gravação anónima das interações nesta página.',
          accept_label: 'Aceitar',
          reject_label: 'Recusar',
          withdraw_label: 'Retirar consentimento',
        },
      },
    },
  }
  const baseConfiguration = browserConfiguration(replayRule)
  const decisions: Array<Record<string, unknown>> = []
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'language', { configurable: true, get: () => 'pt-PT' })
    Object.defineProperty(navigator, 'languages', { configurable: true, get: () => ['pt-PT'] })
  })
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      ...baseConfiguration,
      default_locale: 'pt-pt',
      features: { ...baseConfiguration.features, session_replay: true },
    }),
  }))
  await page.route('**/v1/replay/consents?*', async (route) => {
    decisions.push(route.request().postDataJSON() as Record<string, unknown>)
    return route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        state: 'denied',
        expires_at: '2026-08-23T12:00:00Z',
      }),
    })
  })

  await page.goto('/sample.html')
  await expect(
    page.getByRole('heading', { name: 'Ajude-nos a melhorar esta experiência' }),
  ).toBeVisible()
  await expect(page.getByText('As interações gravadas são conservadas até 30 dias.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Política de privacidade' })).toHaveAttribute(
    'href',
    'https://example.com/privacidade',
  )
  await expect(page.getByRole('button', { name: 'Recusar' })).toBeFocused()
  await page.getByRole('button', { name: 'Recusar' }).click()
  await expect.poll(() => decisions.length).toBe(1)
  expect(decisions[0]).toMatchObject({ locale: 'pt-pt', state: 'denied' })
})

test('anonymous replay suppresses canvas pixels even when a legacy rule requests capture', async ({ page }) => {
  const replayRule = customerReplayRule({
    ...browserRule,
    replay_mode: 'full',
    replay_sampling_rate: 0,
    replay_record_canvas: true,
    replay_canvas_fps: 2,
  })
  const baseConfiguration = browserConfiguration(replayRule)
  const configuration: WidgetConfiguration = {
    ...baseConfiguration,
    features: { ...baseConfiguration.features, session_replay: true },
  }
  const uploadedBatches: Array<{ bytes: number, events: unknown[] }> = []
  let batchIndex = 0
  let finalized = false

  await seedActiveReplayConsent(page)
  await page.addInitScript(() => {
    document.addEventListener('DOMContentLoaded', () => {
      const article = document.querySelector('article')
      if (!article) return
      const included = document.createElement('canvas')
      included.id = 'included-replay-canvas'
      included.width = 80
      included.height = 40
      article.append(included)

      const blocked = document.createElement('div')
      blocked.dataset.fidibekiBlock = ''
      const sensitive = document.createElement('canvas')
      sensitive.id = 'blocked-replay-canvas'
      sensitive.width = 80
      sensitive.height = 40
      blocked.append(sensitive)
      article.append(blocked)

      const includedContext = included.getContext('2d')
      const sensitiveContext = sensitive.getContext('2d')
      const stats = { frames: 0, maxGap: 0, previous: 0 }
      Object.assign(window, { __fidibekiCanvasStats: stats })
      const draw = (now: number) => {
        if (stats.previous > 0) stats.maxGap = Math.max(stats.maxGap, now - stats.previous)
        stats.previous = now
        stats.frames++
        includedContext!.fillStyle = stats.frames % 2 === 0 ? '#e53935' : '#147d64'
        includedContext!.fillRect(0, 0, included.width, included.height)
        sensitiveContext!.fillStyle = '#ff00ff'
        sensitiveContext!.fillRect(0, 0, sensitive.width, sensitive.height)
        requestAnimationFrame(draw)
      }
      requestAnimationFrame(draw)
    }, { once: true })
  })
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/replay/sessions?*', route => route.fulfill({
    status: 201,
    contentType: 'application/json',
    body: JSON.stringify({
      id: 'canvas-session', session_id: 'canvas-session', rrweb_version: '2.1.1',
      max_duration_seconds: 1800, max_events: 50000,
    }),
  }))
  await page.route('**/v1/replay/sessions/canvas-session/batches?*', async (route) => {
    const request = route.request().postDataJSON() as { batch_index: number }
    batchIndex = request.batch_index
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        presigned_url: `http://127.0.0.1:4173/canvas-upload/${batchIndex}`,
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        complete_token: `canvas-token-${batchIndex}`,
      }),
    })
  })
  await page.route('**/canvas-upload/*', async (route) => {
    const body = route.request().postDataBuffer()
    if (!body) throw new Error('canvas replay upload did not contain a body')
    uploadedBatches.push({ bytes: body.byteLength, events: JSON.parse(body.toString()) as unknown[] })
    await route.fulfill({ status: 200 })
  })
  await page.route('**/v1/replay/sessions/canvas-session/batches/*/complete?*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/replay/sessions/canvas-session/finalize?*', (route) => {
    finalized = true
    return route.fulfill({ status: 202, contentType: 'application/json', body: '{}' })
  })

  await page.goto('/sample.html')
  await expect.poll(async () => page.evaluate(() =>
    (window as typeof window & { __fidibekiCanvasStats?: { frames: number } }).__fidibekiCanvasStats?.frames || 0,
  )).toBeGreaterThan(20)
  await page.waitForTimeout(2_200)
  const performance = await page.evaluate(() =>
    (window as typeof window & {
      __fidibekiCanvasStats: { frames: number, maxGap: number }
    }).__fidibekiCanvasStats,
  )
  expect(performance.frames).toBeGreaterThan(40)
  expect(performance.maxGap).toBeLessThan(500)

  await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
  await expect.poll(() => uploadedBatches.length).toBeGreaterThan(0)
  await expect.poll(() => finalized).toBe(true)

  const events = uploadedBatches.flatMap(batch => batch.events) as Array<{
    type?: number
    data?: { source?: number, id?: number }
  }>
  const serialized = JSON.stringify(events)
  const canvasMutations = events.filter(event => event.type === 3 && event.data?.source === 9)
  expect((serialized.match(/"tagName":"canvas"/g) ?? []).length).toBe(1)
  expect(serialized).not.toContain('"property":"drawImage"')
  expect(serialized).not.toContain('included-replay-canvas')
  expect(serialized).not.toContain('blocked-replay-canvas')
  expect(canvasMutations).toHaveLength(0)
  expect(uploadedBatches.every(batch => batch.bytes < 2 * 1024 * 1024)).toBe(true)
  expect(uploadedBatches.reduce((total, batch) => total + batch.bytes, 0)).toBeLessThan(1024 * 1024)
})

for (const consentMode of ['customer_cmp', 'fidibeki_managed'] as const) {
  test(`replay capture is anonymous with ${consentMode} consent: no sentinel identity value reaches storage`, async ({ page }) => {
  const replayRule = (consentMode === 'customer_cmp' ? customerReplayRule : managedReplayRule)({
    ...browserRule, replay_mode: 'full', replay_sampling_rate: 0,
  })
  const baseConfiguration = browserConfiguration(replayRule)
  const configuration: WidgetConfiguration = {
    ...baseConfiguration,
    features: { ...baseConfiguration.features, session_replay: true },
  }

  const sentinels = [
    'SENTINEL-TEXT-Jane-Doe', 'SENTINEL-INPUT-jane.doe@example.com',
    'SENTINEL-TITLE-Jane', 'SENTINEL-ARIA-Jane', 'SENTINEL-ALT-Jane',
    'SENTINEL-AVATAR-jane', 'SENTINEL-ID-jane-42', 'SENTINEL-DATA-jane',
    'SENTINEL-QUERY-jane.doe', 'SENTINEL-QUERY-TOKEN', 'SENTINEL-HASH-fragment',
    'SENTINEL-PATH-jane-private', 'SENTINEL-NAV-SECRET', 'SENTINEL-NAV-HASH',
  ]
  const safeTextMarker = 'SAFE-TEXT-Powered-By-Fidibeki'

  await seedActiveReplayConsent(page, consentMode)
  await page.addInitScript((marker: string) => {
    document.addEventListener('DOMContentLoaded', () => {
      const article = document.querySelector('article')
      if (!article) return

      const text = document.createElement('p')
      text.textContent = 'SENTINEL-TEXT-Jane-Doe'
      article.append(text)

      const input = document.createElement('input')
      input.type = 'email'
      input.value = 'SENTINEL-INPUT-jane.doe@example.com'
      article.append(input)

      const labeled = document.createElement('button')
      labeled.title = 'SENTINEL-TITLE-Jane'
      labeled.setAttribute('aria-label', 'SENTINEL-ARIA-Jane')
      labeled.id = 'SENTINEL-ID-jane-42'
      labeled.dataset.reporter = 'SENTINEL-DATA-jane'
      labeled.textContent = 'Button'
      article.append(labeled)

      const avatar = document.createElement('img')
      avatar.alt = 'SENTINEL-ALT-Jane'
      avatar.src = 'https://cdn.example.test/avatars/SENTINEL-AVATAR-jane.jpg'
      article.append(avatar)

      const safe = document.createElement('div')
      safe.setAttribute('data-fidibeki-safe-text', '')
      safe.textContent = marker
      article.append(safe)
    }, { once: true })
  }, safeTextMarker)

  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configuration),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))

  const sessionRequests: string[] = []
  await page.route('**/v1/replay/sessions?*', async (route) => {
    sessionRequests.push(route.request().postData() ?? '')
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'sentinel-session', session_id: 'sentinel-session', rrweb_version: '2.1.1',
        max_duration_seconds: 1800, max_events: 50000,
      }),
    })
  })
  const uploadedBatches: string[] = []
  await page.route('**/v1/replay/sessions/sentinel-session/batches?*', async (route) => {
    const request = route.request().postDataJSON() as { batch_index: number }
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        presigned_url: `http://127.0.0.1:4173/sentinel-upload/${request.batch_index}`,
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        complete_token: `sentinel-token-${request.batch_index}`,
      }),
    })
  })
  await page.route('**/sentinel-upload/*', async (route) => {
    const body = route.request().postDataBuffer()
    if (!body) throw new Error('replay upload did not contain a body')
    uploadedBatches.push(body.toString())
    await route.fulfill({ status: 200 })
  })
  await page.route('**/v1/replay/sessions/sentinel-session/batches/*/complete?*', route => route.fulfill({ status: 204 }))
  await page.route('**/v1/replay/sessions/sentinel-session/finalize?*', route => route.fulfill({
    status: 202, contentType: 'application/json', body: '{}',
  }))

  await page.goto('/sample.html?email=SENTINEL-QUERY-jane.doe%40example.com&token=SENTINEL-QUERY-TOKEN#SENTINEL-HASH-fragment')
  await page.waitForTimeout(300)
  await page.evaluate(() => {
    history.pushState(
      {},
      '',
      '/users/SENTINEL-PATH-jane-private?token=SENTINEL-NAV-SECRET#SENTINEL-NAV-HASH',
    )
    window.dispatchEvent(new Event('fidibeki:navigation'))
  })
  await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
  await expect.poll(() => uploadedBatches.length).toBeGreaterThan(0)

  expect(sessionRequests.length).toBeGreaterThan(0)
  for (const request of sessionRequests) {
    expect((JSON.parse(request) as { page_url: string }).page_url).toBe(
      'http://127.0.0.1:4173/sample',
    )
  }
  const serializedBatches = uploadedBatches.join('\n')
  for (const sentinel of sentinels) {
    for (const request of sessionRequests) expect(request).not.toContain(sentinel)
    expect(serializedBatches).not.toContain(sentinel)
  }
  // The explicit safe-text opt-in still lets known-non-identifying static text through.
  expect(serializedBatches).toContain(safeTextMarker)
  })
}

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

test('annotates a captured screenshot before attaching it', async ({ page }, testInfo) => {
  await page.goto('/sample.html')
  await page.evaluate(async () => {
    const importModule = new Function('path', 'return import(path)') as (path: string) => Promise<{
      createScreenshotControl(capture: () => Promise<Blob>): { element: HTMLElement; screenshot(): Blob | null }
    }>
    const module = await importModule('/v1/screenshot.js')
    const source = document.createElement('canvas')
    source.width = 960
    source.height = 540
    const context = source.getContext('2d')!
    context.fillStyle = '#f4f7f5'
    context.fillRect(0, 0, source.width, source.height)
    context.fillStyle = '#17201d'
    context.font = 'bold 52px sans-serif'
    context.fillText('Checkout summary', 64, 105)
    context.fillStyle = '#ffffff'
    context.fillRect(64, 150, 832, 300)
    context.strokeStyle = '#d6dfda'
    context.strokeRect(64, 150, 832, 300)
    const blob = await new Promise<Blob>((resolve) => source.toBlob(value => resolve(value!), 'image/png'))
    const control = module.createScreenshotControl(async () => blob)
    document.querySelector('main')!.append(control.element)
    Object.assign(window, { screenshotControl: control })
  })

  await page.getByRole('button', { name: 'Capture screenshot' }).click()
  const editor = page.getByRole('dialog', { name: 'Annotate screenshot' })
  await expect(editor).toBeVisible()
  await expect(editor.getByRole('button', { name: 'Red' })).toHaveAttribute('aria-pressed', 'true')
  await expect(editor.getByRole('button', { name: 'Undo' })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('screenshot-annotator-desktop.png'), animations: 'disabled' })

  const canvas = editor.getByLabel('Screenshot annotation canvas')
  const box = await canvas.boundingBox()
  expect(box).not.toBeNull()
  await page.mouse.move(box!.x + box!.width * 0.3, box!.y + box!.height * 0.35)
  await page.mouse.down()
  await page.mouse.move(box!.x + box!.width * 0.7, box!.y + box!.height * 0.65, { steps: 8 })
  await page.mouse.up()
  await expect(editor.getByRole('button', { name: 'Undo' })).toBeEnabled()
  await editor.getByRole('button', { name: 'Undo' }).click()
  await expect(editor.getByRole('button', { name: 'Undo' })).toBeDisabled()

  await editor.getByRole('button', { name: 'Blue' }).click()
  await page.mouse.move(box!.x + box!.width * 0.2, box!.y + box!.height * 0.6)
  await page.mouse.down()
  await page.mouse.move(box!.x + box!.width * 0.75, box!.y + box!.height * 0.3, { steps: 8 })
  await page.mouse.up()
  await editor.getByRole('button', { name: 'Use screenshot' }).click()

  await expect(page.getByText('Screenshot attached')).toBeVisible()
  await expect(page.getByAltText('Screenshot preview')).toBeVisible()
  const result = await page.evaluate(async () => {
    const control = (window as typeof window & { screenshotControl: { screenshot(): Blob | null } }).screenshotControl
    const screenshot = control.screenshot()
    if (!screenshot) return { size: 0, type: '', blue: 0 }
    const bitmap = await createImageBitmap(screenshot)
    const output = document.createElement('canvas')
    output.width = bitmap.width
    output.height = bitmap.height
    const context = output.getContext('2d')!
    context.drawImage(bitmap, 0, 0)
    const pixel = context.getImageData(Math.round(bitmap.width * 0.2), Math.round(bitmap.height * 0.6), 1, 1).data
    bitmap.close()
    return { size: screenshot.size, type: screenshot.type, blue: pixel[2] ?? 0 }
  })
  expect(result.size).toBeGreaterThan(0)
  expect(result.type).toBe('image/png')
  expect(result.blue).toBeGreaterThan(180)

  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'Edit screenshot' }).click()
  await expect(editor).toBeVisible()
  await expect(editor).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('screenshot-annotator-mobile.png'), animations: 'disabled' })
  await editor.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('button', { name: 'Remove screenshot' }).click()
  await expect(page.getByRole('button', { name: 'Capture screenshot' })).toBeVisible()
  await expect(page.getByText('Screenshot attached')).toBeHidden()
  await expect.poll(() => page.evaluate(() => (
    window as typeof window & { screenshotControl: { screenshot(): Blob | null } }
  ).screenshotControl.screenshot() === null)).toBe(true)
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

test('shows degraded-success guidance when attachment grants are unavailable', async ({ page }) => {
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
    const body = route.request().postDataJSON() as { attachments: unknown[] }
    expect(body.attachments).toHaveLength(1)
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        feedback_id: 'fb_degraded_attachment',
        attachments: [],
        warnings: [{
          code: 'service_unavailable',
          reason: 'upload_rate_limited',
          message: 'Feedback saved without attachments. Try again later.',
          retry_after_seconds: 5,
        }],
      }),
    })
  })

  await page.goto('/sample.html')
  await page.getByRole('button', { name: 'Capture screenshot' }).click()
  await page.getByRole('button', { name: 'Send feedback' }).click()
  await expect(page.getByText('Your feedback was sent.')).toBeVisible()
  await expect(page.getByText('Feedback saved without attachments. Try again later.')).toBeVisible()
  await expect(page.getByText('Could not send feedback. Please try again.')).toHaveCount(0)
})

test('preserves the complete draft and offers retry after a public rate decision', async ({ page }, testInfo) => {
  const retryRule = {
    ...definitionConfiguration('prompt').definitions![0]!,
    can_attach_screenshot: true,
  } satisfies WidgetRule
  const configuration = {
    ...definitionConfiguration('prompt'),
    definitions: [retryRule],
  } satisfies WidgetConfiguration
  const payloads: Array<{
    client_feedback_id: string
    responses: Record<string, string | number>
    reporter_email: string
    notify_reporter: boolean
    consent: { purpose: string; notice_version: string; locale: string; granted: boolean } | null
    attachments: Array<{ client_attachment_id: string; kind: string }>
  }> = []
  await page.route('**/v1/widget/config*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(configuration),
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
    payloads.push(route.request().postDataJSON())
    if (payloads.length === 1) {
      await route.fulfill({
        status: 429,
        contentType: 'application/json',
        headers: { 'Retry-After': '2' },
        body: JSON.stringify({
          error: {
            code: 'rate_limit_exceeded',
            message: 'server copy must not be displayed',
            reason: 'submission_rate_limited',
            retry_after_seconds: 4,
          },
        }),
      })
      return
    }
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ feedback_id: 'fb_retry', attachments: [] }),
    })
  })
  await page.route('**/api/1/envelope/**', (route) => route.fulfill({ status: 200, body: '' }))

  await page.goto('/sample.html')
  await page.getByRole('button', { name: '5 out of 5' }).click()
  await page.getByRole('button', { name: 'Helpful', exact: true }).click()
  await page.getByRole('textbox', { name: 'Details' }).fill('Keep this complete draft.')
  const retryEmail = page.getByRole('textbox', { name: 'Email address' })
  await expect(retryEmail).toBeHidden()
  await page.getByRole('checkbox', { name: 'Email me updates about this feedback' }).check()
  await expect(retryEmail).toBeVisible()
  await retryEmail.fill('reporter@example.test')
  await page.getByRole('button', { name: 'Capture screenshot' }).click()
  await page.getByRole('button', { name: 'Send response' }).click()

  await expect(page.getByText('Too many feedback attempts. Try again in 4 seconds.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Retry' })).toBeDisabled()
  await expect(page.getByRole('textbox', { name: 'Details' })).toHaveValue('Keep this complete draft.')
  await expect(page.getByRole('textbox', { name: 'Email address' })).toHaveValue('reporter@example.test')
  await expect(page.getByRole('checkbox', { name: 'Email me updates about this feedback' })).toBeChecked()
  await expect(page.getByRole('button', { name: 'Screenshot attached' })).toBeVisible()
  await expect(page.getByRole('button', { name: '5 out of 5' })).toHaveAttribute('aria-pressed', 'true')
  await page.screenshot({ path: testInfo.outputPath('retry-state-desktop.png'), animations: 'disabled' })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('retry-state-mobile.png'), animations: 'disabled' })
  await expect(page.getByRole('button', { name: 'Retry' })).toBeEnabled({ timeout: 6_000 })
  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByText('Response received.')).toBeVisible()

  expect(payloads).toHaveLength(2)
  expect(payloads[1]?.client_feedback_id).toBe(payloads[0]?.client_feedback_id)
  expect(payloads[1]?.responses).toEqual(payloads[0]?.responses)
  expect(payloads[1]?.reporter_email).toBe('reporter@example.test')
  expect(payloads[1]?.notify_reporter).toBe(true)
  expect(payloads[1]?.consent).toEqual(payloads[0]?.consent)
  expect(payloads[1]?.consent).toMatchObject({ purpose: 'feedback_updates', granted: true })
  expect(payloads[1]?.attachments).toEqual(payloads[0]?.attachments)
  expect(payloads[1]?.attachments).toHaveLength(1)
})
