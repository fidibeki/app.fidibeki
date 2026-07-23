import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const baseOrigin = `http://localhost:${port}`
const tenantOrigin = `http://acme.localhost:${port}`
const nowDate = new Date()
const now = nowDate.toISOString()
const createdAt = new Date(nowDate.getTime() - 86_400_000).toISOString()

const tenant = {
  id: 'tenant-1',
  name: 'Acme',
  slug: 'acme',
  plan: 'growth',
  status: 'active',
  reporter_access_enabled: true,
  default_locale: 'en',
  default_timezone: 'UTC',
  widget_appearance_defaults: {},
  logo_version: null,
}

const member = {
  id: 'member-1',
  tenant_id: tenant.id,
  email: 'alex@acme.test',
  name: 'Alex Morgan',
  locale: 'en',
  timezone: 'UTC',
  permissions: [...allTenantPermissions],
  status: 'active',
  role: 'owner',
  verified: true,
  avatar_version: null,
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

const issue = {
  id: 'issue-1',
  tenant_id: tenant.id,
  project_id: project.id,
  public_id: 'ISS-2001',
  title: 'Checkout confirmation email never arrives',
  description: 'Customer completed payment but received no confirmation email.',
  status: 'needs_info',
  priority: 'high' as string | null,
  assignee: null,
  tags: [] as Array<{ id: string, name: string, color: string }>,
  merged_into_id: undefined as string | undefined,
  reporter_email: 'client@example.com',
  notify_reporter: true,
  reporter_access_revoked_at: null as string | null,
  resolved_at: undefined as string | undefined,
  feedback_count: 1,
  created_at: createdAt,
  updated_at: now,
}

interface Activity {
  id: string
  issue_id: string
  actor: { id: string, name?: string | null, email: string, avatar_version?: string | null } | null
  kind: string
  visibility: 'internal' | 'reporter'
  data: Record<string, unknown>
  created_at: string
}

const clarificationPrompt = 'Which email address did you use at checkout?'

function progressFixture() {
  return {
    public_id: issue.public_id,
    title: issue.title,
    status: 'needs_info',
    created_at: issue.created_at,
    resolved_at: null,
    events: [{ from_status: 'open', to_status: 'needs_info', created_at: now }],
    clarification: { prompt: clarificationPrompt, responded: false, responded_at: null as string | null },
  }
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath(`${name}.png`), fullPage: false, animations: 'disabled' })
}

test('reporter clarification response submitted on the public page is visible in the backoffice issue timeline', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const token = 'reporter-token-visibility'
  const state = {
    progress: progressFixture(),
    activities: [
      {
        id: 'activity-1', issue_id: issue.id,
        actor: { id: member.id, name: member.name, email: member.email, avatar_version: null },
        kind: 'issue.created', visibility: 'internal' as const, data: {}, created_at: createdAt,
      },
    ] as Activity[],
    lastResponseBody: null as Record<string, unknown> | null,
  }

  await page.route(/^https?:\/\/[^/]+\/v1\/reporter\//, async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === `/v1/reporter/progress/${token}` && method === 'GET') {
      return json(route, { progress: state.progress })
    }
    if (path === `/v1/reporter/progress/${token}/response` && method === 'POST') {
      const body = (request.postDataJSON() || {}) as { response: string }
      state.lastResponseBody = body
      state.progress.clarification.responded = true
      state.progress.clarification.responded_at = now
      state.activities.push({
        id: 'activity-2', issue_id: issue.id, actor: null,
        kind: 'reporter.responded', visibility: 'reporter',
        data: { request_id: 'clarification-1', response: body.response }, created_at: now,
      })
      return json(route, { accepted: true })
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })

  await page.goto(`${baseOrigin}/reporter/${token}`)
  await expect(page.getByRole('heading', { name: issue.title, level: 1 })).toBeVisible()
  await expect(page.getByAltText('Fidibeki')).toBeVisible()

  const responseText = 'Used client@example.com at checkout.\nAlso tried a work email as a backup, same result.'
  await page.getByLabel('Your response').fill(responseText)
  await page.getByRole('button', { name: 'Send response' }).click()
  await expect.poll(() => state.lastResponseBody).toEqual({ response: responseText })
  await expect(page.getByText('Response sent.')).toBeVisible()

  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, {
        access_token: 'visibility-token', token_type: 'Bearer', expires_in: 900, member, tenant,
      })
    }
    if (path === '/v1/projects' && method === 'GET') return json(route, { projects: [project] })
    if (path === '/v1/tags' && method === 'GET') return json(route, { tags: [] })
    if (path === '/v1/members' && method === 'GET') return json(route, { members: [] })
    if (path === '/v1/feedbacks' && method === 'GET') {
      return json(route, { feedbacks: [], pagination: { has_more: false, limit: 20 } })
    }
    if (path === '/v1/issues' && method === 'GET') return json(route, { issues: [issue] })
    if (path === `/v1/issues/${issue.id}` && method === 'GET') {
      return json(route, { issue, updates: [], comments: [], activities: state.activities })
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })

  await page.goto(`${tenantOrigin}/auth/verify?return_to=%2Fissues%2F${issue.id}#token=visibility-token`)
  await expect(page.getByRole('heading', { name: issue.title })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
  await expect(page.getByText('2 entries')).toBeVisible()

  const activityItems = page.locator('.issue-history li')
  const responseEntry = activityItems.filter({ hasText: 'Reporter answered the clarification request' })
  await expect(responseEntry).toBeVisible()
  const responseBlock = responseEntry.locator('blockquote.issue-activity-response')
  await expect(responseBlock).toContainText('Used client@example.com at checkout.')
  await expect(responseBlock).toContainText('Also tried a work email as a backup, same result.')
  const responseTextContent = await responseBlock.evaluate(element => element.textContent || '')
  expect(responseTextContent).toContain('\n')

  const activityOrder = await activityItems.allTextContents()
  expect(activityOrder[0]).toContain('Reporter answered the clarification request')
  expect(activityOrder[activityOrder.length - 1]).toContain('Alex Morgan created the issue')

  const html = await page.locator('.issue-history').innerHTML()
  expect(html).not.toContain('<script')
  await capture(page, testInfo, 'issue-detail-reporter-response')

  if (testInfo.project.name === 'mobile') {
    await page.getByRole('button', { name: 'Open navigation' }).click()
  }
  await page.getByRole('link', { name: 'Issues', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Issues' })).toBeVisible()
  await page.goBack()
  await expect(page.getByRole('heading', { name: issue.title })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
  await expect(responseBlock).toContainText('Used client@example.com at checkout.')
})
