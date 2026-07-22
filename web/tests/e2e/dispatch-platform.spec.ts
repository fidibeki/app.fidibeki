import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = '2026-07-21T12:00:00.000Z'

const planCatalog = [{
  plan: 'starter', monthly_price_cents: 499, yearly_discount_percent: 7,
  limits: { projects: 1, feedback_per_month: 500, users: 1, storage_days: 30 },
  features: { ai: false, session_replay: false, slack: false, video: false, audio: false, dom_capture: false, custom_domain: false },
}, {
  plan: 'growth', monthly_price_cents: 999, yearly_discount_percent: 14,
  limits: { projects: 4, feedback_per_month: 2500, users: 3, storage_days: 90 },
  features: { ai: true, session_replay: true, slack: false, video: false, audio: true, dom_capture: true, custom_domain: false },
}, {
  plan: 'scale', monthly_price_cents: 2799, yearly_discount_percent: 14,
  limits: { projects: 10, feedback_per_month: 10000, users: 10, storage_days: 365 },
  features: { ai: true, session_replay: true, slack: true, video: true, audio: true, dom_capture: true, custom_domain: false },
}]

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
  const members = [member, {
    id: 'member-2',
    tenant_id: tenant.id,
    email: 'jamie@acme.test',
    name: 'Jamie Torres',
    permissions: ['feedback:read', 'feedback:update', 'issue:read', 'issue:update'],
  }]
  const tags = [{ id: 'tag-checkout', name: 'Checkout', color: '#167D68' }, {
    id: 'tag-regression', name: 'Regression', color: '#7C3AED',
  }]
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
    title: 'Checkout froze after I applied a promotion code.',
    description: 'Checkout froze after I applied a promotion code.',
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
    priority: 'high' as 'low' | 'medium' | 'high' | 'urgent' | null,
    assignee: { id: member.id, name: member.name, email: member.email } as { id: string, name?: string, email: string } | null,
    tags: [tags[0]],
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
    title: 'The new checkout is much clearer.',
    description: 'The new checkout is much clearer.',
    text: 'The new checkout is much clearer.',
    text_cleaned: 'The new checkout is much clearer.',
    notify_reporter: false,
    page_url: 'https://shop.acme.test/checkout/success',
    status: 'open',
    ai_status: 'completed',
    priority: null as 'low' | 'medium' | 'high' | 'urgent' | null,
    assignee: null as { id: string, name?: string, email: string } | null,
    tags: [] as typeof tags,
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
    description: 'Customers cannot complete payment after applying some promotion codes.',
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
    description: 'Promotion validation can leave the checkout waiting indefinitely.',
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
    members,
    tags,
    projects,
    widgetRules,
    feedback,
    issues,
    apiKeys: [] as Array<Record<string, unknown>>,
    integrations: [] as Array<Record<string, unknown>>,
    billingDeletionRequestedAt: null as string | null,
    failedReplayFeedbackIDs: new Set<string>(),
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
  const replayFailed = state.failedReplayFeedbackIDs.has(id)
  return {
    ...item,
    dom_snapshot_html: undefined,
    dom_snapshot_styles: undefined,
    attachments: id === 'feedback-1' ? [{
      id: 'attachment-1',
      kind: 'screenshot',
      content_type: 'image/png',
      size_bytes: 2120,
      processing_status: 'failed',
      error_message: 'Screenshot analysis timed out',
      moderation_flags: [],
      processing_attempts: 1,
      completed_at: now,
      processed_at: now,
      created_at: now,
      content_url: `/v1/feedbacks/${id}/attachments/attachment-1/content`,
    }, {
      id: 'attachment-2',
      kind: 'audio',
      content_type: 'audio/webm',
      size_bytes: 4096,
      processing_status: 'failed',
      error_message: 'attachment upload expired before completion',
      moderation_flags: [],
      processing_attempts: 0,
      processed_at: now,
      created_at: now,
    }] : [],
    session: id === 'feedback-1' ? {
      id: 'session-1',
      status: replayFailed ? 'failed' : 'ready',
      page_url: item.page_url,
      started_at: '2026-07-21T09:58:00.000Z',
      ended_at: '2026-07-21T10:00:00.000Z',
      events_count: replayFailed ? 0 : 3,
      batches_count: replayFailed ? 0 : 1,
      rrweb_version: '2.0',
      replay_ready: !replayFailed,
      replay_url: replayFailed ? undefined : '/v1/sessions/session-1/replay',
      merge_error: replayFailed ? 'No replay events were uploaded before the recording ended.' : undefined,
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
    const projectRuleMatch = path.match(/^\/v1\/projects\/([^/]+)\/widget-rules\/([^/]+)$/)
    if (projectRuleMatch && method === 'PATCH') {
      const rule = state.widgetRules.find(candidate => candidate.project_id === projectRuleMatch[1] && candidate.id === projectRuleMatch[2])
      if (rule) Object.assign(rule, body(), { updated_at: now })
      return json(route, { widget_rule: rule })
    }
    const projectMatch = path.match(/^\/v1\/projects\/([^/]+)$/)
    if (projectMatch && method === 'GET') {
      return json(route, { project: state.projects.find(project => project.id === projectMatch[1]) })
    }
    if (projectMatch && method === 'PATCH') {
      const project = state.projects.find(candidate => candidate.id === projectMatch[1])
      if (project) Object.assign(project, body(), { updated_at: now })
      return json(route, { project })
    }

    if (path === '/v1/feedbacks' && method === 'GET') {
      const requestedStatus = url.searchParams.get('status')
      const requestedAttachment = url.searchParams.get('attachment')
      const requestedQuery = url.searchParams.get('q')?.toLowerCase()
      const items = state.feedback.filter((item) => {
        if (requestedQuery && ![item.public_id, item.title, item.description, item.issue_public_id]
          .some(value => value?.toLowerCase().includes(requestedQuery))) return false
        if (requestedStatus && item.status !== requestedStatus) return false
        if (requestedAttachment === 'replay') return item.has_replay
        if (requestedAttachment === 'none') return item.attachment_kinds.length === 0 && !item.has_replay
        if (requestedAttachment) return (item.attachment_kinds as string[]).includes(requestedAttachment)
        return true
      })
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
      const input = body()
      if (item && input.status) item.status = String(input.status)
      if (item && input.title) item.title = String(input.title)
      if (item && input.priority) item.priority = input.priority === 'none' ? null : String(input.priority) as typeof item.priority
      if (item && input.unassign) item.assignee = null
      if (item && input.assignee_id) {
        const assignee = state.members.find(candidate => candidate.id === input.assignee_id)
        item.assignee = assignee ? { id: assignee.id, name: assignee.name, email: assignee.email } : null
      }
      if (item && Array.isArray(input.remove_tag_ids)) {
        item.tags = item.tags.filter(tag => !(input.remove_tag_ids as string[]).includes(tag.id))
      }
      if (item && Array.isArray(input.add_tag_ids)) {
        for (const tagID of input.add_tag_ids as string[]) {
          const tag = state.tags.find(candidate => candidate.id === tagID)
          if (tag && !item.tags.some(candidate => candidate.id === tag.id)) item.tags.push(tag)
        }
        item.tags.sort((left, right) => left.name.localeCompare(right.name))
      }
      return json(route, { feedback: item })
    }
    const feedbackIssueMatch = path.match(/^\/v1\/feedbacks\/([^/]+)\/issue$/)
    if (feedbackIssueMatch && method === 'POST') {
      const feedbackItem = state.feedback.find(candidate => candidate.id === feedbackIssueMatch[1])
      const input = body()
      const issue = {
        id: 'issue-created', tenant_id: state.tenant.id, project_id: feedbackItem?.project_id || 'project-1',
        public_id: 'ISS-102', title: String(input.title), description: String(input.description || ''),
        status: 'open', reporter_email: feedbackItem?.reporter_email || '', notify_reporter: feedbackItem?.notify_reporter || false,
        feedback_count: 1, created_at: now, updated_at: now, merged_into_id: undefined as string | undefined,
      }
      state.issues.push(issue)
      if (feedbackItem) {
        feedbackItem.issue_id = issue.id
        feedbackItem.issue_public_id = issue.public_id
        feedbackItem.issue_status = 'open'
      }
      return json(route, { issue }, 201)
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

    if (path === '/v1/plans' && method === 'GET') return json(route, { plans: planCatalog })
    if (path === '/v1/config' && method === 'GET') return json(route, {
      tenant: state.tenant,
      plan: planCatalog.find(plan => plan.plan === state.tenant.plan),
    })
    if (path === '/v1/members' && method === 'GET') return json(route, { members: state.members })
    if (path === '/v1/tags' && method === 'GET') return json(route, { tags: state.tags })
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

test('dashboard distinguishes unrated feedback from a zero rating', async ({ page }, testInfo) => {
  const state = createMockState()
  delete (state.feedback[1] as Partial<(typeof state.feedback)[number]>).rating
  await authenticate(page, state)

  const ratedRow = page.locator('.dashboard-work-row').filter({ hasText: 'FB-1048' })
  const unratedRow = page.locator('.dashboard-work-row').filter({ hasText: 'FB-1047' })
  await expect(ratedRow.getByTitle('2 out of 5')).toBeVisible()
  await expect(unratedRow.getByTitle('Not rated')).toBeVisible()
  await expect(unratedRow.locator('.rating-mark > [aria-hidden="true"]')).toHaveText('—')
  await expect(page.getByTitle('0 out of 5')).toHaveCount(0)
  await capture(page, testInfo, 'dispatch-dashboard-unrated')
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
  const newProjectButton = page.getByRole('button', { name: 'New project' })
  await newProjectButton.click()
  const dialog = page.getByRole('dialog', { name: 'Create project' })
  await expect(dialog.getByLabel('Project name')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await expect(newProjectButton).toBeFocused()
  await newProjectButton.click()
  await dialog.getByLabel('Project name').fill('Documentation')
  await dialog.getByLabel('Allowed domains').fill('docs.acme.test\n*.docs-preview.acme.test')
  await dialog.getByRole('button', { name: 'Create project' }).click()
  await expect(page).toHaveURL(/\/projects\/project-2$/)
  await expect(page.getByText('Documentation', { exact: true }).first()).toBeVisible()
  expect(state.projects).toHaveLength(2)
})

test('starter plan exposes exhausted project capacity before form entry', async ({ page }, testInfo) => {
  const state = createMockState()
  state.tenant.plan = 'starter'
  await authenticate(page, state)
  await openPrimary(page, 'Projects')

  await expect(page.getByRole('button', { name: 'New project' })).toHaveCount(0)
  await expect(page.getByText('1 of 1 projects used')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Upgrade plan' })).toHaveAttribute('href', '/billing')
  await capture(page, testInfo, 'starter-project-limit')
})

test('starter plan shows unavailable features and euro-denominated prices', async ({ page }, testInfo) => {
  const state = createMockState()
  state.tenant.plan = 'starter'
  await authenticate(page, state)
  await openPrimary(page, 'Settings')

  await expect(page.getByText('€4.99/month')).toBeVisible()
  const aiFeature = page.locator('.settings-features li').filter({ hasText: /^ai$/i })
  await expect(aiFeature.locator('svg')).toHaveClass(/lucide-x/)
  await expect(page.locator('.settings-features .lucide-check')).toHaveCount(0)
  await capture(page, testInfo, 'starter-entitlements')

  await page.getByRole('link', { name: 'Manage billing' }).click()
  await expect(page.getByRole('heading', { name: 'Billing', exact: true })).toBeVisible()
  await expect(page.locator('.billing-plan').filter({ hasText: 'Starter' })).toContainText('€4.99')
  await expect(page.locator('.billing-plans')).not.toContainText('$')
  await capture(page, testInfo, 'billing-euro-catalog')
})

test('starter feedback without evidence does not offer invalid AI reprocessing', async ({ page }, testInfo) => {
  const state = createMockState()
  state.tenant.plan = 'starter'
  state.feedback.splice(0, 1)
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  if (testInfo.project.name === 'mobile') await page.getByRole('link', { name: 'Open FB-1047' }).click()
  else await page.getByTitle('Open full detail').click()

  await expect(page.getByText('No media was attached to this feedback.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reprocess all' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Upgrade for AI' })).toHaveAttribute('href', '/billing')
  await capture(page, testInfo, 'starter-empty-evidence')
})

test('widget rule save enforces browser validation before sending an update', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Native form validation is viewport independent')
  const state = createMockState()
  let updateRequests = 0
  page.on('request', (request) => {
    if (request.method() === 'PATCH' && new URL(request.url()).pathname.endsWith('/widget-rules/rule-1')) updateRequests += 1
  })
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()

  const name = page.getByLabel('Name', { exact: true })
  await name.fill('')
  await page.getByRole('button', { name: 'Save rule' }).click()

  await expect(name).toBeFocused()
  expect(updateRequests).toBe(0)

  await name.fill('Checkout feedback')
  const maxDuration = page.getByLabel('Max duration (seconds)')
  const maxEvents = page.getByLabel('Max events')
  await expect(maxDuration).toHaveAttribute('max', '1800')
  await expect(maxEvents).toHaveAttribute('max', '50000')

  await maxDuration.fill('1801')
  await page.getByRole('button', { name: 'Save rule' }).click()
  await expect(maxDuration).toBeFocused()
  expect(updateRequests).toBe(0)

  await maxDuration.fill('1800')
  await maxEvents.fill('50001')
  await page.getByRole('button', { name: 'Save rule' }).click()
  await expect(maxEvents).toBeFocused()
  expect(updateRequests).toBe(0)
})

test('widget rule save submits a valid update', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Rule submission is viewport independent')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()

  await page.getByLabel('Name', { exact: true }).fill('Checkout follow-up')
  await page.getByRole('button', { name: 'Save rule' }).click()

  await expect(page.getByRole('status')).toHaveText('Widget rule saved.')
  expect(state.widgetRules[0].name).toBe('Checkout follow-up')
})

test('project settings enforce browser validation before sending an update', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Native form validation is viewport independent')
  const state = createMockState()
  let updateRequests = 0
  page.on('request', (request) => {
    if (request.method() === 'PATCH' && new URL(request.url()).pathname === '/v1/projects/project-1') updateRequests += 1
  })
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()

  const name = page.getByLabel('Project name')
  await name.fill('')
  await page.getByRole('button', { name: 'Save project' }).click()

  await expect(name).toBeFocused()
  expect(updateRequests).toBe(0)
})

test('project settings submit a valid update', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Project submission is viewport independent')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()

  await page.getByLabel('Project name').fill('European Storefront')
  await page.getByRole('button', { name: 'Save project' }).click()

  await expect(page.getByRole('status')).toHaveText('Project settings saved.')
  expect(state.projects[0].name).toBe('European Storefront')
})

test('widget preview controls stay out of keyboard and accessibility navigation', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'The preview is hidden on smaller viewports')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()

  const preview = page.locator('.widget-preview-page')
  const receivedFocus = await preview.locator('button, textarea').first().evaluate((control) => {
    control.focus()
    return document.activeElement === control
  })

  expect(receivedFocus).toBe(false)
  await expect(preview.getByRole('button')).toHaveCount(0)
})

test('feedback can be inspected, previewed, and archived', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  await page.getByTitle('Open full detail').click()
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/)
  await expect(page.getByRole('heading', { name: 'Checkout froze after I applied a promotion code.' })).toBeVisible()

  await page.getByRole('button', { name: 'View' }).click()
  await expect(page.getByRole('dialog', { name: 'Attachment evidence' })).toBeVisible()
  await expect(page.getByAltText('Captured feedback screenshot')).toBeVisible()
  await page.getByTitle('Close evidence').click()

  await page.getByRole('button', { name: 'Archive' }).click()
  await expect(page.getByText('Feedback marked archived.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reopen' })).toBeVisible()
  expect(state.feedback[0].status).toBe('archived')
})

test('unlinked feedback can be titled and promoted into an editable issue', async ({ page }, testInfo) => {
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  if (testInfo.project.name === 'mobile') {
    await page.getByRole('link', { name: 'Open FB-1047' }).click()
  } else {
    await page.getByRole('listitem').filter({ hasText: 'FB-1047' }).click()
    await page.getByTitle('Open full detail').click()
  }

  await expect(page.getByRole('heading', { name: 'The new checkout is much clearer.' })).toBeVisible()
  await expect(page.getByText('The new checkout is much clearer.', { exact: true })).toHaveCount(2)
  await page.getByTitle('Edit feedback title').click()
  await page.getByLabel('Feedback title').fill('Checkout success page is clearer')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Checkout success page is clearer' })).toBeVisible()

  await page.getByRole('button', { name: 'Create issue' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Create issue from feedback' })).toBeVisible()
  await expect(dialog.getByLabel('Title')).toHaveValue('Checkout success page is clearer')
  await dialog.getByLabel('Description').fill('Customers confirm that the redesigned success page is easier to understand.')
  await dialog.getByRole('button', { name: 'Create issue' }).click()

  await expect(page).toHaveURL(/\/issues\/issue-created$/)
  await expect(page.getByRole('heading', { name: 'Checkout success page is clearer' })).toBeVisible()
  await expect(page.getByText('Customers confirm that the redesigned success page is easier to understand.')).toBeVisible()
  expect(state.feedback[1].issue_id).toBe('issue-created')
  await capture(page, testInfo, 'issue-created-from-feedback')
})

test('expired uploads are not offered an AI processing retry', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Attachment retry behavior is viewport independent')
  const state = createMockState()
  const retryRequests: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && /^\/v1\/attachments\/[^/]+\/retry$/.test(path)) retryRequests.push(path)
  })
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  await page.getByTitle('Open full detail').click()
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/)

  const expiredUpload = page.locator('.feedback-attachment-list article').filter({ hasText: 'attachment upload expired before completion' })
  await expect(expiredUpload).toBeVisible()
  await expect(expiredUpload.getByRole('button', { name: 'Retry', exact: true })).toHaveCount(0)
  await expect(page.getByText('1 attachment can be retried.')).toBeVisible()

  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Attachment retry queued.')
  expect(retryRequests).toEqual(['/v1/attachments/attachment-1/retry'])
})

test('terminal replay failures are shown as unavailable', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Replay terminal-state behavior is viewport independent')
  const state = createMockState()
  state.failedReplayFeedbackIDs.add('feedback-1')
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  await page.getByTitle('Open full detail').click()
  await expect(page.getByRole('heading', { name: 'Checkout froze after I applied a promotion code.' })).toBeVisible()
  await page.getByRole('button', { name: 'Replay', exact: true }).click()

  await expect(page.getByRole('heading', { name: 'Session replay unavailable' })).toBeVisible()
  await expect(page.getByText('No replay events were uploaded before the recording ended.')).toBeVisible()
  await expect(page.locator('.feedback-replay-surface .status-pill')).toHaveText('failed')
})

test('feedback queue links apply their filters without a document reload', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Queue routing is viewport independent')
  const state = createMockState()
  state.feedback[1].status = 'archived'
  let documentRequests = 0
  page.on('request', (request) => {
    if (request.resourceType() === 'document') documentRequests += 1
  })
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')
  const documentRequestsBeforeQueueNavigation = documentRequests

  const queues = page.getByRole('navigation', { name: 'Feedback queues' })
  await queues.getByRole('link', { name: 'Has replay' }).click()

  await expect(page).toHaveURL(/\/feedback\?.*attachment=replay/)
  await expect(page.getByLabel('Evidence filter')).toHaveValue('replay')
  await expect(page.getByLabel('Status filter')).toHaveValue('open')
  await expect(page.getByText('1 loaded')).toBeVisible()

  await queues.getByRole('link', { name: 'Archived' }).click()

  await expect(page).toHaveURL(`${tenantOrigin}/feedback?status=archived`)
  await expect(page.getByLabel('Status filter')).toHaveValue('archived')
  await expect(page.getByLabel('Evidence filter')).toHaveValue('')
  await expect(page.getByText('1 loaded')).toBeVisible()
  await expect(page.locator('.feedback-stream')).toContainText('FB-1047')
  expect(documentRequests).toBe(documentRequestsBeforeQueueNavigation)
})

test('feedback inspector persists operational triage metadata', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'The inspector is a desktop workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')

  const inspector = page.locator('.feedback-inspector')
  await expect(inspector.getByRole('group', { name: 'Priority' })).toBeVisible()
  await inspector.getByRole('button', { name: 'Urgent priority' }).click()
  await inspector.getByLabel('Assignee').selectOption('member-2')
  await inspector.getByLabel('Regression tag').check()
  await inspector.getByLabel('Checkout tag').uncheck()

  expect(state.feedback[0].priority).toBe('urgent')
  expect(state.feedback[0].assignee?.id).toBe('member-2')
  expect(state.feedback[0].tags.map(tag => tag.id)).toEqual(['tag-regression'])

  await page.reload()
  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)
  await openPrimary(page, 'Feedback')
  await expect(inspector.getByRole('button', { name: 'Urgent priority' })).toHaveAttribute('aria-pressed', 'true')
  await expect(inspector.getByLabel('Assignee')).toHaveValue('member-2')
  await expect(inspector.getByLabel('Regression tag')).toBeChecked()
  const row = page.getByRole('listitem').filter({ hasText: 'FB-1048' })
  await expect(row.locator('.feedback-priority-mark')).toHaveText('Urgent')
  await expect(row.locator('.feedback-assignee-mark')).toHaveText('JT')
  await capture(page, testInfo, 'feedback-operational-triage')
})

test('all-status and clear-filters choices survive route synchronization', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Filter routing is viewport independent')
  const state = createMockState()
  state.feedback[1].status = 'archived'
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')

  const statusFilter = page.getByLabel('Status filter')
  await statusFilter.selectOption('')

  await expect(statusFilter).toHaveValue('')
  await expect(page.getByText('2 loaded')).toBeVisible()
  expect(new URL(page.url()).searchParams.has('status')).toBe(false)
  await page.locator('.feedback-stream article').filter({ hasText: 'FB-1047' }).click()
  await expect(statusFilter).toHaveValue('')
  await expect(page.getByText('2 loaded')).toBeVisible()
  expect(new URL(page.url()).searchParams.has('status')).toBe(false)

  await statusFilter.selectOption('archived')
  await expect(page.getByText('1 loaded')).toBeVisible()
  await page.getByText('More filters', { exact: true }).click()
  await page.getByRole('button', { name: 'Clear filters' }).click()

  await expect(statusFilter).toHaveValue('')
  await expect(page.getByText('2 loaded')).toBeVisible()
  expect(new URL(page.url()).searchParams.has('status')).toBe(false)
})

test('read-only feedback rows preserve grid placement', async ({ page }) => {
  const state = createMockState()
  state.member.permissions = ['feedback:read', 'issue:read']
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')

  const row = page.getByRole('listitem').first()
  await expect(row).toBeVisible()
  const placement = await row.evaluate((element) => {
    const rating = element.querySelector<HTMLElement>('.rating-mark')!
    const style = getComputedStyle(element)
    return {
      actual: rating.getBoundingClientRect().left - element.getBoundingClientRect().left,
      expected: parseFloat(style.paddingLeft)
        + parseFloat(style.gridTemplateColumns.split(/\s+/)[0]!)
        + parseFloat(style.columnGap),
    }
  })
  expect(placement.actual).toBeCloseTo(placement.expected, 0)
})

test('read-only members can inspect feedback without mutation controls', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Permission behavior is viewport independent')
  const state = createMockState()
  state.member.permissions = ['feedback:read', 'issue:read']
  await authenticate(page, state)
  await openPrimary(page, 'Feedback')

  await expect(page.getByRole('checkbox', { name: /^Select FB-/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Archive', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Spam', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Reprocess AI', exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Open issue', exact: true })).toBeVisible()
  await expect(page.locator('.feedback-inspector').getByText('High', { exact: true })).toBeVisible()
  await expect(page.locator('.feedback-inspector').getByText('Alex Morgan', { exact: true })).toBeVisible()
  await expect(page.locator('.feedback-inspector').getByRole('group', { name: 'Priority' })).toHaveCount(0)
  await expect(page.locator('.feedback-inspector').getByLabel('Assignee')).toHaveCount(0)

  await page.getByTitle('Open full detail').click()
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/)
  await expect(page.getByRole('button', { name: 'Archive', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Spam', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Reprocess all', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'View', exact: true })).toBeVisible()
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

test('issue detail uses singular feedback copy for one linked record', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Copy behavior is viewport independent')
  const state = createMockState()
  state.issues[0].feedback_count = 1
  state.feedback.splice(1)
  await authenticate(page, state)
  await openPrimary(page, 'Issues')
  await page.locator('.issues-table').getByRole('link', { name: /ISS-101/ }).click()

  await expect(page.locator('.issue-detail-heading')).toContainText('1 feedback record · created')
  await expect(page.locator('.issue-feedback > header > span')).toHaveText('1 record')
})

test('issue status can change without nesting the control inside its detail link', async ({ page }, testInfo) => {
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Issues')

  await expect(page.locator('.issues-table a select')).toHaveCount(0)
  await capture(page, testInfo, 'dispatch-issues')
  await page.getByLabel('Status for ISS-101').selectOption('resolved')

  await expect(page).toHaveURL(`${tenantOrigin}/issues`)
  await expect(page.getByLabel('Status for ISS-101')).toHaveValue('resolved')
  expect(state.issues[0].status).toBe('resolved')
})

test('settings creates one-time credentials and delivery secrets', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Covered once as an interaction workflow')
  const state = createMockState()
  await authenticate(page, state)
  await openPrimary(page, 'Settings')

  await page.getByRole('link', { name: 'API keys' }).click()
  await expect(page).toHaveURL(/\/settings\?tab=keys$/)
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible()
  const createKeyButton = page.getByRole('button', { name: 'Create key' })
  await createKeyButton.click()
  const keyDialog = page.getByRole('dialog', { name: 'Create API key' })
  await expect(keyDialog.getByLabel('Name')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(keyDialog).not.toBeVisible()
  await expect(createKeyButton).toBeFocused()
  await createKeyButton.click()
  await keyDialog.getByLabel('Name').fill('Warehouse exporter')
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
  await expect(dialog.getByLabel(/Type acme to confirm/)).toBeFocused()
  await dialog.getByLabel(/Type acme to confirm/).fill('acme')
  await dialog.getByRole('button', { name: 'Schedule deletion' }).click()
  await expect(page.getByText('Deletion scheduled', { exact: true })).toBeVisible()
  await expect(page.getByText('This workspace is pending deletion')).toBeVisible()

  await page.getByRole('button', { name: 'Cancel deletion' }).click()
  await expect(page.getByText('Deletion scheduled', { exact: true })).toHaveCount(0)
  expect(state.billingDeletionRequestedAt).toBeNull()
})

test('pending deletion keeps feedback readable and cancellation available', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Workspace lifecycle behavior is viewport independent')
  const state = createMockState()
  state.tenant.deletion_requested_at = now
  state.billingDeletionRequestedAt = now
  const mutationRequests: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if ((request.method() === 'PATCH' && /^\/v1\/feedbacks\/[^/]+$/.test(path)) ||
      (request.method() === 'POST' && /\/reprocess$/.test(path))) {
      mutationRequests.push(`${request.method()} ${path}`)
    }
  })
  await authenticate(page, state)

  await openPrimary(page, 'Feedback')
  await expect(page.getByLabel(/Select FB-/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Archive', exact: true })).toHaveCount(0)
  await page.getByTitle('Open full detail').click()
  await expect(page.getByRole('heading', { name: 'Checkout froze after I applied a promotion code.' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reprocess all' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Spam' })).toHaveCount(0)
  expect(mutationRequests).toEqual([])

  await page.getByRole('link', { name: 'Review deletion' }).click()
  await expect(page.getByRole('button', { name: 'Billing portal' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Cancel deletion' }).click()
  await expect(page.getByText('This workspace is pending deletion')).toHaveCount(0)
  expect(state.billingDeletionRequestedAt).toBeNull()
})

test('manage-only member can reach billing and cancel pending deletion', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Permission routing is viewport independent')
  const state = createMockState()
  state.member.permissions = ['billing:manage']
  state.tenant.deletion_requested_at = now
  state.billingDeletionRequestedAt = now
  await installMockAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)

  await expect(page).toHaveURL(`${tenantOrigin}/billing`)
  await expect(page.getByRole('heading', { name: 'Billing', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel deletion' }).click()
  await expect(page.getByText('This workspace is pending deletion')).toHaveCount(0)
  expect(state.billingDeletionRequestedAt).toBeNull()
})

test('pending deletion disables mutations across workspace administration', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Workspace lifecycle behavior is viewport independent')
  const state = createMockState()
  state.tenant.deletion_requested_at = now
  state.billingDeletionRequestedAt = now
  state.apiKeys.push({
    id: 'key-1', tenant_id: state.tenant.id, name: 'Reporting', token_prefix: 'fk_live_report',
    permissions: ['feedback:read'], rate_limit_per_min: 100, expires_at: null,
    last_used_at: null, created_at: now, updated_at: now,
  })
  state.integrations.push({
    id: 'integration-1', kind: 'webhook', name: 'Operations',
    webhook_url: 'https://hooks.acme.test/fidibeki', channel: null,
    webhook_secret_fingerprint: 'sha256:demo', custom_header_names: [],
    events: ['feedback.created'], is_active: true, created_at: now, updated_at: now,
  })
  await authenticate(page, state)

  await openPrimary(page, 'Issues')
  await expect(page.getByLabel('Status for ISS-101')).toBeDisabled()
  await page.getByRole('link', { name: /Open ISS-101:/ }).click()
  await expect(page.getByRole('button', { name: 'Update issue' })).toBeDisabled()
  await expect(page.locator('.issue-update-panel textarea')).toBeDisabled()
  await expect(page.locator('.issue-merge select')).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Merge', exact: true })).toBeDisabled()

  await openPrimary(page, 'Projects')
  await expect(page.getByRole('button', { name: 'New project' })).toHaveCount(0)
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()
  await expect(page.getByTitle('New widget rule')).toHaveCount(0)
  await expect(page.getByTitle('Delete rule')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Save rule' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Save project' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Delete project' })).toBeDisabled()
  await expect(page.getByLabel('Name', { exact: true })).toBeDisabled()
  await expect(page.getByLabel('Project name')).toBeDisabled()

  await openPrimary(page, 'Settings')
  await page.getByRole('link', { name: 'Members' }).click()
  await expect(page.getByRole('button', { name: 'Invite member' })).toHaveCount(0)
  await expect(page.getByTitle('Edit permissions')).toHaveCount(0)
  await page.getByRole('link', { name: 'API keys' }).click()
  await expect(page.getByRole('button', { name: 'Create key' })).toHaveCount(0)
  await expect(page.getByTitle('Revoke API key')).toHaveCount(0)
  await page.getByRole('link', { name: 'Integrations' }).click()
  await expect(page.getByRole('button', { name: 'Add integration' })).toHaveCount(0)
  await expect(page.getByTitle('Remove integration')).toHaveCount(0)
})

test('pending deletion disables first-project activation', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Workspace lifecycle behavior is viewport independent')
  const state = createMockState()
  state.projects.splice(0)
  state.widgetRules.splice(0)
  state.tenant.deletion_requested_at = now
  state.billingDeletionRequestedAt = now
  await installMockAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`)
  await expect(page).toHaveURL(`${tenantOrigin}/getting-started`)
  await expect(page.getByRole('heading', { name: 'Connect your first project' })).toBeVisible()
  const form = page.locator('.activation-surface form')
  await expect(form.getByLabel('Project name')).toBeDisabled()
  await expect(form.locator('button[type="submit"]')).toBeDisabled()
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
  if (testInfo.project.name === 'desktop') {
    await expect(page.getByTitle('Workspace settings')).toHaveAttribute('href', '/settings')
    await expect(page.getByTitle('Notifications')).toHaveCount(0)
  }
  if (testInfo.project.name === 'mobile') {
    const primaryNavigation = page.getByRole('navigation', { name: 'Primary navigation' })
    const openNavigation = page.getByTitle('Open navigation')
    await expect(primaryNavigation).not.toBeVisible()
    await openNavigation.click()
    await expect(primaryNavigation).toBeVisible()
    await expect(page.getByTitle('Close navigation')).toBeFocused()
    await expect(page.locator('.app-main')).toHaveAttribute('inert', '')
    await page.keyboard.press('Escape')
    await expect(primaryNavigation).not.toBeVisible()
    await expect(openNavigation).toBeFocused()
  }
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
  await expect(page.getByRole('button', { name: 'Play or pause replay' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Toggle fullscreen replay' })).toBeVisible()
  await expect(page.getByRole('checkbox', { name: 'Skip inactive periods' })).toBeVisible()
  await capture(page, testInfo, 'dispatch-feedback-replay')

  await page.getByRole('link', { name: 'Back to queue' }).click()
  await expect(page).toHaveURL(/\/feedback(?:\?|$)/)
  await openPrimary(page, 'Projects')
  await page.locator('.project-list').getByRole('link', { name: /Storefront/ }).click()
  await expect(page).toHaveURL(/\/projects\/project-1$/)
  await expect(page.getByRole('heading', { name: 'Checkout pulse', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Delete rule' })).toBeVisible()
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
