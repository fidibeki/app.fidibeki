import { expect, test, type Route } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'
import { visibleLayoutDefects } from './layout-assertions'

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = '2026-07-21T12:00:00.000Z'

test.use({
  extraHTTPHeaders: { 'Accept-Language': 'en-XA,en;q=0.8' },
  locale: 'en-XA',
  timezoneId: 'UTC',
})

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

test('pseudo-locale product surfaces remain responsive and preserve customer content', async ({ page }, testInfo) => {
  const tenant = {
    id: 'tenant-1',
    name: 'Acme Studio',
    slug: 'acme',
    plan: 'growth',
    status: 'active',
    reporter_access_enabled: true,
    default_locale: 'en',
    default_timezone: 'UTC',
  }
  const member = {
    id: 'member-1',
    tenant_id: tenant.id,
    email: 'alex@acme.test',
    name: 'Alex Morgan',
    locale: 'en-XA',
    timezone: 'UTC',
    permissions: [...allTenantPermissions],
    status: 'active',
    role: 'owner',
    verified: true,
  }
  const project = {
    id: 'project-1',
    tenant_id: tenant.id,
    name: 'Storefront',
    public_key: 'pk_live_storefront',
    allowed_domains: ['shop.acme.test'],
    default_locale: 'en',
    reporter_access_enabled: null,
    widget_appearance_overrides: {},
    widget_last_seen_at: now,
    created_at: now,
    updated_at: now,
  }
  const feedback = {
    id: 'feedback-1',
    public_id: 'FB-1048',
    project_id: project.id,
    project_name: project.name,
    rating: 2,
    title: 'Checkout froze after applying a promotion code.',
    description: 'Checkout froze after applying a promotion code.',
    text: 'Checkout froze after applying a promotion code.',
    text_cleaned: 'Checkout froze after applying a promotion code.',
    page_url: 'https://shop.acme.test/checkout',
    status: 'open',
    ai_status: 'completed',
    priority: 'high',
    assignee: null,
    tags: [],
    sentiment_score: -0.74,
    moderation_flags: [],
    attachment_kinds: ['screenshot'],
    has_replay: false,
    issue_id: null,
    issue_public_id: null,
    created_at: now,
    updated_at: now,
  }
  const issue = {
    id: 'issue-1',
    tenant_id: tenant.id,
    project_id: project.id,
    public_id: 'ISS-1042',
    title: 'Promotion codes make checkout unresponsive.',
    description: 'The checkout button stops responding after a promotion code is applied.',
    status: 'open',
    priority: 'high',
    assignee: null,
    tags: [],
    merged_into_id: null,
    reporter_email: 'customer@example.test',
    notify_reporter: true,
    reporter_access_revoked_at: null,
    resolved_at: null,
    feedback_count: 1,
    created_at: now,
    updated_at: now,
  }
  const configuration = {
    tenant,
    plan: {
      plan: 'growth',
      monthly_price_cents: 4900,
      yearly_discount_percent: 20,
      limits: {
        projects: 10,
        feedback_per_month: 10_000,
        users: 10,
        storage_days: 90,
      },
      features: {
        ai: true,
        session_replay: true,
        slack: true,
        video: false,
        audio: true,
        dom_capture: true,
        custom_domain: false,
      },
    },
  }
  const widgetRule = {
    id: 'rule-1',
    project_id: project.id,
    name: 'Checkout pulse',
    priority: 10,
    enabled: true,
    matcher_type: 'url_prefix',
    matcher_value: 'https://shop.acme.test/checkout',
    include_conditions: [{
      id: 'condition-1',
      matcher_type: 'url_prefix',
      matcher_value: 'https://shop.acme.test/checkout',
      position: 0,
    }],
    exclude_conditions: [],
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
    replay_mode: 'none',
    replay_sampling_rate: 0,
    replay_max_duration_seconds: 1800,
    replay_max_events: 50_000,
    replay_mask_all_inputs: true,
    replay_mask_text_selector: '.private',
    replay_record_canvas: false,
    replay_canvas_fps: 2,
    recurrence_mode: 'cooldown',
    recurrence_days: 30,
    definition_key: 'checkout-pulse',
    definition_version: 1,
    definition_status: 'active',
    presentation: 'launcher',
    objective: 'continuous_feedback',
    response_blocks: [
      { key: 'rating', type: 'rating', required: false },
      { key: 'feedback', type: 'free_text', required: false },
    ],
    localizations: {
      en: {
        launcher_label: 'Feedback',
        prompt: 'How was checkout?',
        comment_placeholder: 'Tell us more',
        contact_label: 'Email address',
        consent_label: 'Email me about this feedback',
        submit_label: 'Send feedback',
        success_message: 'Your feedback was sent.',
      },
    },
    allow_comment: false,
    collect_contact: false,
    appearance_overrides: {},
    created_at: now,
    updated_at: now,
  }

  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const path = new URL(request.url()).pathname
    if ((path === '/auth/verify' || path === '/auth/refresh') && request.method() === 'POST') {
      return json(route, {
        access_token: 'pseudo-access-token',
        token_type: 'Bearer',
        expires_in: 900,
        member,
        tenant,
      })
    }
    if (path === '/v1/dashboard' && request.method() === 'GET') {
      return json(route, {
        dashboard: {
          range_days: 30,
          feedback_count: 128,
          feedback_delta_percent: 12.4,
          open_issue_count: 3,
          satisfaction: 4.2,
          satisfaction_delta: 0.3,
          attention_count: 2,
          trend: [
            { date: '2026-07-20', feedback_count: 17, satisfaction: 4.2 },
            { date: '2026-07-21', feedback_count: 21, satisfaction: 4.4 },
          ],
        },
      })
    }
    if (path === '/v1/feedbacks' && request.method() === 'GET') {
      return json(route, { feedbacks: [feedback], next_cursor: null })
    }
    if (path === `/v1/issues/${issue.id}` && request.method() === 'GET') {
      return json(route, {
        issue,
        updates: [],
        comments: [],
        activities: [{
          id: 'activity-1',
          issue_id: issue.id,
          actor: null,
          kind: 'issue.created',
          visibility: 'internal',
          data: {},
          created_at: now,
        }],
      })
    }
    if (path === '/v1/issues' && request.method() === 'GET') {
      return json(route, { issues: [issue] })
    }
    if (path === '/v1/projects' && request.method() === 'GET') {
      return json(route, { projects: [project] })
    }
    if (path === `/v1/projects/${project.id}` && request.method() === 'GET') {
      return json(route, { project })
    }
    if (path === `/v1/projects/${project.id}/widget-rules` && request.method() === 'GET') {
      return json(route, { widget_rules: [widgetRule] })
    }
    if (path === '/v1/config' && request.method() === 'GET') {
      return json(route, configuration)
    }
    if (path === '/v1/tags' && request.method() === 'GET') {
      return json(route, { tags: [] })
    }
    if (path === '/v1/members' && request.method() === 'GET') {
      return json(route, { members: [member] })
    }
    if (path === '/v1/member-domains' && request.method() === 'GET') {
      return json(route, { member_domains: [] })
    }
    if (path === '/v1/api-keys' && request.method() === 'GET') {
      return json(route, { api_keys: [] })
    }
    if (path === '/v1/integrations' && request.method() === 'GET') {
      return json(route, { integrations: [] })
    }
    if (path === '/v1/organization-profile' && request.method() === 'GET') {
      return json(route, {
        organization_profile: {
          name: tenant.name,
          short_name: null,
          website_url: 'https://acme.test',
          support_email: 'support@acme.test',
          accent_color: '#087f73',
          default_locale: 'en',
          default_timezone: 'UTC',
          privacy_policy_url: null,
          terms_url: null,
          logo_version: null,
        },
      })
    }
    return json(route, { error: { code: 'not_found', message: 'Not found' } }, 404)
  })

  await page.goto(`${tenantOrigin}/auth/verify#token=pseudo-token`)

  await expect(page).toHaveURL(`${tenantOrigin}/`)
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Alex')
  await expect(page.getByText('Checkout froze after applying a promotion code.')).toBeVisible()
  await expect(page.getByText('Storefront', { exact: true }).first()).toBeVisible()
  if ((page.viewportSize()?.width || 0) <= 700) {
    const search = page.getByRole('searchbox')
    await page.getByRole('search').click()
    await expect(search).toBeFocused()
    await expect(search).toHaveCSS('opacity', '1')
    expect((await search.boundingBox())?.width).toBeGreaterThan(200)
  }
  expect(await visibleLayoutDefects(page)).toEqual([])
  await page.screenshot({
    path: testInfo.outputPath('pseudo-dashboard.png'),
    animations: 'disabled',
    fullPage: true,
  })

  const surfaces = [
    {
      path: '/feedback',
      customerText: feedback.title,
      screenshot: 'pseudo-feedback.png',
    },
    {
      path: '/issues',
      customerText: issue.title,
      screenshot: 'pseudo-issues.png',
    },
    {
      path: `/issues/${issue.id}`,
      customerText: issue.description,
      localizedSelector: '.issue-update-panel h2',
      screenshot: 'pseudo-issue-detail.png',
    },
    {
      path: '/projects',
      customerText: project.name,
      screenshot: 'pseudo-projects.png',
    },
    {
      path: `/projects/${project.id}`,
      customerText: widgetRule.name,
      localizedSelector: '.project-rule-editor form h3',
      screenshot: 'pseudo-project-editor.png',
    },
    {
      path: '/settings',
      customerText: tenant.name,
      screenshot: 'pseudo-settings.png',
    },
    {
      path: '/profile',
      customerText: member.name,
      screenshot: 'pseudo-profile.png',
    },
  ]

  for (const surface of surfaces) {
    const link = page.locator(`a[href="${surface.path}"]`).first()
    await expect(link).toHaveCount(1)
    await link.evaluate((element: HTMLAnchorElement) => element.click())
    await expect(page).toHaveURL(`${tenantOrigin}${surface.path}`)
    await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA')
    await expect(page.getByText(surface.customerText, { exact: true }).filter({ visible: true }).first()).toBeVisible()
    await expect(page.locator(surface.localizedSelector || 'h1').first()).toContainText('[')
    expect(await visibleLayoutDefects(page)).toEqual([])
    await page.screenshot({
      path: testInfo.outputPath(surface.screenshot),
      animations: 'disabled',
      fullPage: true,
    })
  }
})
