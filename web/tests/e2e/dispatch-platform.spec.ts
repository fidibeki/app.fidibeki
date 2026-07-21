import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = '2026-07-21T12:00:00.000Z'

type MockState = ReturnType<typeof createMockState>

function createMockState() {
  const tenant = {
    id: 'tenant-1',
    name: 'Acme Studio',
    slug: 'acme',
    plan: 'growth',
    status: 'trial',
    trial_ends_at: '2026-08-04T12:00:00.000Z',
    deletion_requested_at: null as string | null,
  }
  const member = {
    id: 'member-1',
    tenant_id: tenant.id,
    email: 'alex@acme.test',
    name: 'Alex Morgan',
    permissions: [...allTenantPermissions],
  }
  const projects = [{
    id: 'project-1',
    tenant_id: tenant.id,
    name: 'Storefront',
    public_key: 'pk_live_storefront',
    allowed_domains: ['shop.acme.test', '*.preview.acme.test'],
    created_at: now,
    updated_at: now,
  }]
  const widgetRules = [{
    id: 'rule-1',
    project_id: projects[0].id,
    name: 'Checkout pulse',
    priority: 10,
    enabled: true,
    matcher_type: 'url_prefix',
    matcher_value: 'https://shop.acme.test/checkout',
    trigger_type: 'delay',
    trigger_delay_seconds: 4,
    trigger_scroll_percent: null,
    can_rate: true,
    can_write_text: true,
    can_record_audio: true,
    can_record_video: false,
    can_attach_element: true,
    can_attach_screenshot: true,
    greeting: 'How was checkout?',
    position: 'bottom-right',
    theme: 'light',
    accent_color: '#087f73',
    replay_mode: 'full',
    replay_sampling_rate: 0,
    replay_max_duration_seconds: 1800,
    replay_max_events: 50000,
    replay_mask_all_inputs: true,
    replay_mask_text_selector: '.private',
    created_at: now,
    updated_at: now,
  }]
  const feedback = [{
    id: 'feedback-1',
    public_id: 'FB-1048',
    project_id: projects[0].id,
    project_name: projects[0].name,
    widget_rule_id: widgetRules[0].id,
    session_id: 'session-1',
    issue_id: 'issue-1',
    issue_public_id: 'ISS-101',
    issue_status: 'open',
    rating: 2,
    text: 'Checkout froze after I applied a promotion code.',
    text_cleaned: 'Checkout froze after I applied a promotion code.',
    reporter_email: 'customer@example.test',
    notify_reporter: true,
    page_url: 'https://shop.acme.test/checkout',
    referrer: 'https://shop.acme.test/cart',
    user_agent: 'Mozilla/5.0',
    os: 'macOS',
    browser: 'Chrome',
    device: 'desktop',
    viewport_width: 1280,
    viewport_height: 720,
    language: 'en-US',
    status: 'open',
    ai_status: 'completed',
    sentiment_score: -0.74,
    moderation_flags: [],
    attachment_kinds: ['screenshot', 'replay'],
    has_replay: true,
    created_at: '2026-07-21T10:00:00.000Z',
    updated_at: now,
  }, {
    id: 'feedback-2',
    public_id: 'FB-1047',
    project_id: projects[0].id,
    project_name: projects[0].name,
    rating: 5,
    text: 'The new checkout is much clearer.',
    text_cleaned: 'The new checkout is much clearer.',
    notify_reporter: false,
    page_url: 'https://shop.acme.test/checkout/success',
    status: 'open',
    ai_status: 'completed',
    sentiment_score: 0.86,
    moderation_flags: [],
    attachment_kinds: [],
    has_replay: false,
    created_at: '2026-07-21T09:00:00.000Z',
    updated_at: now,
  }]
  const issues = [{
    id: 'issue-1',
    tenant_id: tenant.id,
    project_id: projects[0].id,
    public_id: 'ISS-101',
    title: 'Promotion code can freeze checkout',
    status: 'open',
    reporter_email: 'customer@example.test',
    notify_reporter: true,
    feedback_count: 3,
    created_at: '2026-07-19T12:00:00.000Z',
    updated_at: now,
    merged_into_id: undefined as string | undefined,
  }, {
    id: 'issue-2',
    tenant_id: tenant.id,
    project_id: projects[0].id,
    public_id: 'ISS-099',
    title: 'Checkout stalls during promotion validation',
    status: 'in_progress',
    reporter_email: 'ops@acme.test',
    notify_reporter: false,
    feedback_count: 7,
    created_at: '2026-07-16T12:00:00.000Z',
    updated_at: now,
    merged_into_id: undefined as string | undefined,
  }]
  return {
    tenant,
    member,
    projects,
    widgetRules,
    feedback,
    issues,
    apiKeys: [] as Array<Record<string, unknown>>,
    integrations: [] as Array<Record<string, unknown>>,
    billingDeletionRequestedAt: null as string | null,
  }
}

function authResponse(state: MockState) {
  return {
    access_token: 'test-access-token',
    token_type: 'Bearer',
    expires_in: 900,
    member: state.member,
    tenant: state.tenant,
  }
}

function feedbackDetail(state: MockState, id: string) {
  const item = state.feedback.find(candidate => candidate.id === id) || state.feedback[0]
  return {
    ...item,
    dom_snapshot_html: undefined,
    dom_snapshot_styles: undefined,
    attachments: id === 'feedback-1' ? [{
      id: 'attachment-1',
      kind: 'screenshot',
      content_type: 'image/png',
      size_bytes: 2120,
      processing_status: 'completed',
      moderation_flags: [],
      processing_attempts: 1,
      completed_at: now,
      processed_at: now,
      created_at: now,
      content_url: `/v1/feedbacks/${id}/attachments/attachment-1/content`,
    }] : [],
    session: id === 'feedback-1' ? {
      id: 'session-1',
      page_url: item.page_url,
      started_at: '2026-07-21T09:58:00.000Z',
      ended_at: '2026-07-21T10:00:00.000Z',
      events_count: 3,
      batches_count: 1,
      rrweb_version: '2.0',
      replay_ready: true,
      replay_url: '/v1/sessions/session-1/replay',
    } : undefined,
    ai_processed_at: now,
  }
}

const replayEvents = [
  { type: 4, data: { href: 'https://shop.acme.test/checkout', width: 1280, height: 720 }, timestamp: 1_000 },
  {
    type: 2,
    data: {
      node: {
        type: 0,
        id: 1,
        childNodes: [{ type: 1, name: 'html', publicId: '', systemId: '', id: 2 }, {
          type: 2,
          tagName: 'html',
          attributes: {},
          id: 3,
          childNodes: [{ type: 2, tagName: 'head', attributes: {}, id: 4, childNodes: [] }, {
            type: 2,
            tagName: 'body',
            attributes: { style: 'font-family:Arial;padding:32px;background:#fff' },
            id: 5,
            childNodes: [{
              type: 2,
              tagName: 'h1',
              attributes: {},
              id: 6,
              childNodes: [{ type: 3, textContent: 'Checkout', id: 7 }],
            }, {
              type: 2,
              tagName: 'p',
              attributes: {},
              id: 8,
              childNodes: [{ type: 3, textContent: 'Review your order', id: 9 }],
            }],
          }],
        }],
      },
      initialOffset: { left: 0, top: 0 },
    },
    timestamp: 1_001,
  },
  { type: 1, data: {}, timestamp: 1_050 },
]

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function installMockAPI(page: Page, state: MockState) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    const body = () => request.postDataJSON() as Record<string, unknown>

    if (path === '/auth/login' && method === 'POST') return json(route, {}, 202)
    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') return json(route, authResponse(state))
    if (path === '/auth/logout' && method === 'POST') return json(route, {})

    if (path === '/v1/dashboard' && method === 'GET') {
      return json(route, { dashboard: {
        range_days: 30,
        feedback_count: 128,
        feedback_delta_percent: 12.4,
        open_issue_count: state.issues.filter(issue => issue.status !== 'closed').length,
        satisfaction: 4.2,
        satisfaction_delta: 0.3,
        attention_count: 2,
        trend: [
          { date: '2026-07-17', feedback_count: 9, satisfaction: 4.0 },
          { date: '2026-07-18', feedback_count: 13, satisfaction: 4.1 },
          { date: '2026-07-19', feedback_count: 8, satisfaction: 4.3 },
          { date: '2026-07-20', feedback_count: 17, satisfaction: 4.2 },
          { date: '2026-07-21', feedback_count: 21, satisfaction: 4.4 },
        ],
      } })
    }

    if (path === '/v1/projects' && method === 'GET') return json(route, { projects: state.projects })
    if (path === '/v1/projects' && method === 'POST') {
      const input = body()
      const project = {
        id: `project-${state.projects.length + 1}`,
        tenant_id: state.tenant.id,
        name: String(input.name),
        public_key: `pk_live_project_${state.projects.length + 1}`,
        allowed_domains: input.allowed_domains as string[],
        created_at: now,
        updated_at: now,
      }
      state.projects.push(project)
      return json(route, { project }, 201)
    }
    const projectRulesMatch = path.match(/^\/v1\/projects\/([^/]+)\/widget-rules$/)
    if (projectRulesMatch && method === 'GET') {
      return json(route, { widget_rules: state.widgetRules.filter(rule => rule.project_id === projectRulesMatch[1]) })
    }
    const projectMatch = path.match(/^\/v1\/projects\/([^/]+)$/)
    if (projectMatch && method === 'GET') {
      return json(route, { project: state.projects.find(project => project.id === projectMatch[1]) })
    }

    if (path === '/v1/feedbacks' && method === 'GET') {
      const requestedStatus = url.searchParams.get('status')
      const items = state.feedback.filter(item => !requestedStatus || item.status === requestedStatus)
      return json(route, { feedbacks: items, pagination: { next_cursor: null, has_more: false, limit: 50 } })
    }
    const attachmentMatch = path.match(/^\/v1\/feedbacks\/([^/]+)\/attachments\/([^/]+)\/content$/)
    if (attachmentMatch && method === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'image/png',
        body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEAQH/2p7iWQAAAABJRU5ErkJggg==', 'base64'),
      })
    }
    const feedbackMatch = path.match(/^\/v1\/feedbacks\/([^/]+)$/)
    if (feedbackMatch && method === 'GET') return json(route, { feedback: feedbackDetail(state, feedbackMatch[1]) })
    if (feedbackMatch && method === 'PATCH') {
      const item = state.feedback.find(candidate => candidate.id === feedbackMatch[1])
      if (item && body().status) item.status = String(body().status)
      return json(route, { feedback: item })
    }
    if (/^\/v1\/feedbacks\/[^/]+\/reprocess$/.test(path) && method === 'POST') return json(route, {}, 202)
    if (/^\/v1\/attachments\/[^/]+\/retry$/.test(path) && method === 'POST') return json(route, {}, 202)
    if (path === '/v1/sessions/session-1/replay' && method === 'GET') return json(route, replayEvents)

    if (path === '/v1/issues' && method === 'GET') return json(route, { issues: state.issues })
    const issueMergeMatch = path.match(/^\/v1\/issues\/([^/]+)\/merge$/)
    if (issueMergeMatch && method === 'POST') {
      const issue = state.issues.find(candidate => candidate.id === issueMergeMatch[1])
      if (issue) issue.merged_into_id = String(body().target_issue_id)
      return json(route, { issue })
    }
    if (issueMergeMatch && method === 'DELETE') {
      const issue = state.issues.find(candidate => candidate.id === issueMergeMatch[1])
      if (issue) issue.merged_into_id = undefined
      return json(route, { issue })
    }
    const issueMatch = path.match(/^\/v1\/issues\/([^/]+)$/)
    if (issueMatch && method === 'GET') {
      return json(route, { issue: state.issues.find(issue => issue.id === issueMatch[1]), updates: [] })
    }
    if (issueMatch && method === 'PATCH') {
      const issue = state.issues.find(candidate => candidate.id === issueMatch[1])
      if (issue && body().status) issue.status = String(body().status)
      return json(route, { issue })
    }

    if (path === '/v1/config' && method === 'GET') return json(route, {
      tenant: state.tenant,
      plan: {
        plan: state.tenant.plan,
        monthly_price_cents: 999,
        yearly_discount_percent: 15,
        limits: { projects: 4, feedback_per_month: 5000, users: 3, storage_days: 90 },
        features: { ai: true, session_replay: true, slack: true, video: false, audio: true, dom_capture: true, custom_domain: false },
      },
    })
    if (path === '/v1/members' && method === 'GET') return json(route, { members: [state.member] })
    if (path === '/auth/invite' && method === 'POST') return json(route, {}, 202)
    if (path === '/v1/api-keys' && method === 'GET') return json(route, { api_keys: state.apiKeys })
    if (path === '/v1/api-keys' && method === 'POST') {
      const input = body()
      const key = {
        id: 'key-1', tenant_id: state.tenant.id, name: input.name,
        token_prefix: 'fk_live_demo...', permissions: input.permissions,
        rate_limit_per_min: input.rate_limit_per_min, expires_at: input.expires_at,
        last_used_at: null, created_at: now, updated_at: now,
      }
      state.apiKeys.push(key)
      return json(route, { api_key: key, token: 'fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ' }, 201)
    }
    if (path === '/v1/integrations' && method === 'GET') return json(route, { integrations: state.integrations })
    if (path === '/v1/integrations' && method === 'POST') {
      const input = body()
      const integration = {
        id: 'integration-1', kind: input.kind, name: input.name, webhook_url: input.webhook_url,
        channel: input.channel, webhook_secret_fingerprint: 'sha256:demo', custom_header_names: [],
        events: input.events, is_active: true, created_at: now, updated_at: now,
      }
      state.integrations.push(integration)
      return json(route, { integration, webhook_secret: 'whsec_dispatch_demo' }, 201)
    }

    if (path === '/v1/billing' && method === 'GET') return json(route, { billing: {
      plan: state.tenant.plan,
      status: state.tenant.status,
      trial_ends_at: state.tenant.trial_ends_at,
      has_customer: false,
      has_subscription: false,
      deletion_requested_at: state.billingDeletionRequestedAt,
    } })
    if (path === '/v1/billing/delete-account' && method === 'POST') {
      state.billingDeletionRequestedAt = now
      state.tenant.deletion_requested_at = now
      return json(route, {}, 202)
    }
    if (path === '/v1/billing/cancel-deletion' && method === 'POST') {
      state.billingDeletionRequestedAt = null
      state.tenant.deletion_requested_at = null
      return json(route, {})
    }

    return json(route, { error: { code: 'mock_route_missing', message: `${method} ${path} is not mocked` } }, 404)
  })
}

async function authenticate(page: Page, state: MockState) {
  await installMockAPI(page, state)
  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)
  await expect(page.getByRole('heading', { name: /Good afternoon/ })).toBeVisible()
}

async function openPrimary(page: Page, label: 'Feedback' | 'Issues' | 'Projects' | 'Settings') {
  const link = page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: label, exact: true })
  if ((page.viewportSize()?.width || 0) <= 700) await page.getByTitle('Open navigation').click()
  await link.click()
  const path = { Feedback: '/feedback', Issues: '/issues', Projects: '/projects', Settings: '/settings' }[label]
  await expect(page).toHaveURL(new RegExp(`${path}(?:\\?|$)`))
  await expect(page.getByRole('heading', { name: label, exact: true })).toBeVisible()
}

async function expectNoPageOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }))
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth + 1)
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  await expect(page.locator('body')).not.toContainText('Internal Server Error')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: testInfo.outputPath(`${name}.png`), fullPage: false, animations: 'disabled' })
}

test('magic-link login enters the Dispatch workspace', async ({ page }) => {
  const state = createMockState()
  await installMockAPI(page, state)
  await page.goto(`${tenantOrigin}/login`)
  await page.waitForLoadState('networkidle')
  await page.getByLabel('Work email').fill('alex@acme.test')
  await page.getByRole('button', { name: 'Send sign-in link' }).click()
  await expect(page.getByRole('status')).toContainText('Check your inbox')

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)
  await expect(page).toHaveURL(`${tenantOrigin}/`)
  await expect(page.getByRole('heading', { name: 'Good afternoon, Alex' })).toBeVisible()
})

test('restricted members enter their first permitted workspace section', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Permission routing is viewport independent')
  const state = createMockState()
  state.member.permissions = ['issue:read', 'issue:update', 'project:read']
  await installMockAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)

  await expect(page).toHaveURL(`${tenantOrigin}/issues`)
  const navigation = page.getByRole('navigation', { name: 'Primary navigation' })
  await expect(navigation.getByRole('link', { name: 'Issues', exact: true })).toBeVisible()
  await expect(navigation.getByRole('link', { name: 'Projects', exact: true })).toBeVisible()
  await expect(navigation.getByRole('link', { name: 'Overview', exact: true })).toHaveCount(0)
  await expect(navigation.getByRole('link', { name: 'Feedback', exact: true })).toHaveCount(0)
  await expect(navigation.getByRole('link', { name: 'Settings', exact: true })).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'Feedback queues' })).toHaveCount(0)
  await expect(page.getByRole('search')).toHaveCount(0)
})

test('project creation opens the new collection workspace', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.getByRole('button', { name: 'New project' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create project' })
  await dialog.getByLabel('Project name').fill('Documentation')
  await dialog.getByLabel('Allowed domains').fill('docs.acme.test\n*.docs-preview.acme.test')
  await dialog.getByRole('button', { name: 'Create project' }).click()
  await expect(page).toHaveURL(/\/projects\/project-2$/)
  await expect(page.getByText('Documentation', { exact: true }).first()).toBeVisible()
  expect(state.projects).toHaveLength(2)
})

test('feedback can be inspected, previewed, and archived', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  await page.getByTitle('Open full detail').click()
  await expect(page.getByRole('heading', { name: 'FB-1048' })).toBeVisible()

  await page.getByRole('button', { name: 'View' }).click()
  await expect(page.getByRole('dialog', { name: 'Attachment evidence' })).toBeVisible()
  await expect(page.getByAltText('Captured feedback screenshot')).toBeVisible()
  await page.getByTitle('Close evidence').click()

  await page.getByRole('button', { name: 'Archive' }).click()
  await expect(page.getByText('Feedback marked archived.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reopen' })).toBeVisible()
  expect(state.feedback[0].status).toBe('archived')
})

test('issues merge into an existing project issue', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Issues')
  await page.locator('.issues-table').getByRole('link', { name: /ISS-101/ }).click()
  await expect(page).toHaveURL(/\/issues\/issue-1$/)
  await page.getByRole('combobox').filter({ has: page.locator('option', { hasText: 'Choose target issue' }) }).selectOption('issue-2')
  await page.getByRole('button', { name: 'Merge' }).click()
  await expect(page.getByText('This issue is merged into another issue.')).toBeVisible()
  expect(state.issues[0].merged_into_id).toBe('issue-2')
})

test('settings creates one-time credentials and delivery secrets', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Settings')

  await page.getByRole('link', { name: 'API keys' }).click()
  await expect(page).toHaveURL(/\/settings\?tab=keys$/)
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Create key' }).click()
  await page.getByRole('dialog', { name: 'Create API key' }).getByLabel('Name').fill('Warehouse exporter')
  await page.getByRole('dialog', { name: 'Create API key' }).getByRole('button', { name: 'Create key' }).click()
  await expect(page.getByRole('heading', { name: 'API key created' })).toBeVisible()
  expect(state.apiKeys).toHaveLength(1)
  await page.getByRole('button', { name: 'Done' }).click()

  await page.getByRole('link', { name: 'Integrations' }).click()
  await expect(page).toHaveURL(/\/settings\?tab=integrations$/)
  await expect(page.getByRole('heading', { name: 'Integrations', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Add integration' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add integration' })
  await dialog.getByLabel('Name').fill('Product operations')
  await dialog.getByLabel('Webhook URL').fill('https://hooks.acme.test/fidibeki')
  await dialog.getByRole('button', { name: 'Add integration' }).click()
  await expect(page.getByRole('heading', { name: 'Signing secret' })).toBeVisible()
  expect(state.integrations).toHaveLength(1)
})

test('workspace deletion supports the full recovery path', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await page.locator('.app-account-menu summary').click()
  await page.getByRole('link', { name: 'Billing', exact: true }).click()
  await page.getByRole('button', { name: 'Schedule deletion' }).click()
  const dialog = page.getByRole('dialog', { name: 'Schedule workspace deletion' })
  await dialog.getByLabel(/Type acme to confirm/).fill('acme')
  await dialog.getByRole('button', { name: 'Schedule deletion' }).click()
  await expect(page.getByText('Deletion scheduled', { exact: true })).toBeVisible()
  await expect(page.getByText('This workspace is pending deletion')).toBeVisible()

  await page.getByRole('button', { name: 'Cancel deletion' }).click()
  await expect(page.getByText('Deletion scheduled', { exact: true })).toHaveCount(0)
  expect(state.billingDeletionRequestedAt).toBeNull()
})

test('Dispatch production surfaces remain responsive and error-free', async ({ page }, testInfo) => {
  const state = createMockState()
  const browserErrors: string[] = []
  page.on('pageerror', error => browserErrors.push(error.message))
  page.on('console', message => {
    const text = message.text()
    const expectedSandboxBlock = text.startsWith('Blocked script execution in ') && text.includes("frame is sandboxed and the 'allow-scripts' permission is not set")
    if (message.type() === 'error' && !expectedSandboxBlock) browserErrors.push(text)
  })
  await authenticate(page, state)
  await capture(page, testInfo, 'dispatch-overview')

  await openPrimary(page, 'Feedback')
  await expect(page.locator('.feedback-stream article').first()).toContainText('FB-1048')
  await capture(page, testInfo, 'dispatch-feedback')
  if (testInfo.project.name === 'mobile') {
    await page.getByRole('link', { name: 'Open FB-1048' }).click()
  } else {
    await page.getByTitle('Open full detail').click()
  }
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/)
  await expect(page.locator('.feedback-detail-page').getByRole('heading', { name: 'Checkout froze after I applied a promotion code.' })).toBeVisible()
  await page.getByRole('button', { name: 'Replay' }).click()
  await expect(page.getByText('3 events', { exact: true })).toBeVisible()
  await capture(page, testInfo, 'dispatch-feedback-replay')

  await page.getByRole('link', { name: 'Back to queue' }).click()
  await expect(page).toHaveURL(/\/feedback(?:\?|$)/)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()
  await expect(page).toHaveURL(/\/projects\/project-1$/)
  await expect(page.getByRole('heading', { name: 'Checkout pulse', exact: true })).toBeVisible()
  await capture(page, testInfo, 'dispatch-project')

  await openPrimary(page, 'Settings')
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
  await capture(page, testInfo, 'dispatch-settings')

  await page.locator('.app-account-menu summary').click()
  await page.getByRole('link', { name: 'Billing', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Billing', exact: true })).toBeVisible()
  if (testInfo.project.name === 'mobile') {
    const rowAlignment = await page.locator('.billing-plan').first().locator('dl > div').first().evaluate((row) => {
      const value = row.querySelector('dd')
      const rowBox = row.getBoundingClientRect()
      const valueBox = value?.getBoundingClientRect()
      return { rowLeft: rowBox.left, rowWidth: rowBox.width, valueLeft: valueBox?.left || 0 }
    })
    expect(rowAlignment.valueLeft).toBeGreaterThan(rowAlignment.rowLeft + rowAlignment.rowWidth * 0.7)
  }
  await capture(page, testInfo, 'dispatch-billing')
  expect(browserErrors).toEqual([])
})
