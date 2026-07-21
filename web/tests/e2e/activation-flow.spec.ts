import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const now = '2026-07-21T12:00:00.000Z'
const plans = [{
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

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function clientNavigate(page: Page, path: string) {
  await page.goto(path)
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  await page.screenshot({ path: testInfo.outputPath(`${name}.png`), fullPage: true })
}

test('public signup keeps form state and resets Turnstile after a conflict', async ({ page }, testInfo) => {
  let signupAttempts = 0
  let turnstileScriptAttempts = 0
  let submittedBody: Record<string, unknown> = {}
  await page.route('**/turnstile/**', (route) => {
    turnstileScriptAttempts += 1
    if (turnstileScriptAttempts === 1) return route.abort('failed')
    return route.fulfill({
      contentType: 'application/javascript',
      body: `window.__turnstileResets=0;window.__turnstileOptions=null;window.turnstile={render:function(el,options){window.__turnstileOptions=options;el.textContent='Verified';setTimeout(function(){options.callback('test-token')},0);return 'signup-widget'},reset:function(){window.__turnstileResets++;setTimeout(function(){window.__turnstileOptions.callback('reset-token')},0)},remove:function(){}};`,
    })
  })
  await page.route('**/v1/plans', route => json(route, { plans }))
  await page.route('**/v1/signup', async (route) => {
    signupAttempts += 1
    submittedBody = route.request().postDataJSON() as Record<string, unknown>
    if (signupAttempts === 1) return json(route, { error: { code: 'conflict', message: 'tenant slug already exists' } }, 409)
    return json(route, { tenant: {
      id: 'tenant-new', name: 'Acme Studio', slug: 'acme-studio', plan: 'growth', status: 'trial',
      trial_ends_at: '2026-08-04T12:00:00.000Z',
    } }, 201)
  })

  await clientNavigate(page, '/signup')
  await expect(page.getByRole('heading', { name: 'Create your workspace' })).toBeVisible()
  await page.waitForFunction(() => Boolean((document.querySelector('#__nuxt') as Element & { __vue_app__?: unknown })?.__vue_app__))
  const retryPlans = page.getByTitle('Retry loading plans')
  await expect(page.locator('.signup-plan-grid').or(retryPlans)).toBeVisible()
  if (await retryPlans.isVisible()) await retryPlans.click()
  await expect(page.locator('.signup-plan-grid')).toContainText('Growth')
  await expect(page.getByText('Security verification could not be loaded. Please try again.')).toBeVisible()
  await page.getByRole('button', { name: 'Retry verification' }).click()
  await expect(page.getByText('Verified', { exact: true })).toBeVisible()
  await page.getByLabel('Workspace name').fill('Acme Studio')
  await page.getByLabel('Owner email').fill('owner@acme.test')
  await expect(page.getByLabel('Workspace URL')).toHaveValue('acme-studio')
  await page.getByRole('button', { name: 'Create workspace' }).click()
  await expect.poll(() => signupAttempts).toBe(1)
  await expect(page.getByText('tenant slug already exists')).toBeVisible()
  await expect(page.getByLabel('Workspace name')).toHaveValue('Acme Studio')
  await expect.poll(() => page.evaluate(() => (window as typeof window & { __turnstileResets?: number }).__turnstileResets || 0)).toBe(1)

  await page.getByLabel('Workspace URL').fill('acme-studio')
  await page.getByRole('button', { name: 'Create workspace' }).click()
  await expect(page.getByRole('heading', { name: 'Check owner@acme.test' })).toBeVisible()
  expect(submittedBody).toMatchObject({ slug: 'acme-studio', plan: 'growth', turnstile_token: 'reset-token' })
  await capture(page, testInfo, 'signup-confirmation')
})

test('magic link activates a project and detects its widget connection', async ({ page }, testInfo) => {
  const tenant = {
    id: 'tenant-new', name: 'Acme Studio', slug: 'acme', plan: 'growth', status: 'trial',
    trial_ends_at: '2026-08-04T12:00:00.000Z', deletion_requested_at: null,
  }
  const member = {
    id: 'member-new', tenant_id: tenant.id, email: 'owner@acme.test', name: 'Owner',
    permissions: [...allTenantPermissions],
  }
  const projects: Array<Record<string, unknown>> = []
  let detailRequests = 0
  let createdRule: Record<string, unknown> = {}

  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (value: string) => { (window as typeof window & { __copiedInstall?: string }).__copiedInstall = value } },
    })
  })
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/auth/verify' && method === 'POST') return json(route, {
      access_token: 'activation-token', token_type: 'Bearer', expires_in: 900, member, tenant,
    })
    if (path === '/v1/projects' && method === 'GET') return json(route, { projects })
    if (path === '/v1/projects' && method === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      const project = {
        id: 'project-new', tenant_id: tenant.id, name: body.name, public_key: 'pk_live_activationkey000000',
        allowed_domains: body.allowed_domains, widget_last_seen_at: null, created_at: now, updated_at: now,
      }
      projects.push(project)
      return json(route, { project }, 201)
    }
    if (path === '/v1/projects/project-new/widget-rules' && method === 'POST') {
      createdRule = request.postDataJSON() as Record<string, unknown>
      return json(route, { widget_rule: { id: 'rule-new', project_id: 'project-new', ...createdRule, created_at: now, updated_at: now } }, 201)
    }
    if (path === '/v1/projects/project-new' && method === 'GET') {
      detailRequests += 1
      return json(route, { project: { ...projects[0], widget_last_seen_at: now } })
    }
    return json(route, { error: { code: 'not_found', message: `Unhandled ${method} ${path}` } }, 404)
  })

  await page.goto('/auth/verify#token=activation-magic-link')
  await expect(page).toHaveURL(/\/getting-started$/)
  await expect(page.getByRole('heading', { name: 'Connect your first project' })).toBeVisible()
  await page.getByLabel('Allowed domains').fill('example.com\n*.preview.example.com')
  await page.getByRole('button', { name: 'Create and configure' }).click()
  await expect(page.getByText('Install loader')).toBeVisible()
  expect(createdRule).toMatchObject({
    enabled: true, matcher_type: 'all_pages', trigger_delay_seconds: 0,
    can_rate: true, can_write_text: true, can_record_audio: false, replay_mode: 'none',
  })
  await expect(page.locator('.activation-snippet')).toContainText('http://widget.localhost:8088/v1/loader.js')
  await page.getByTitle('Copy install code').click()
  await expect.poll(() => page.evaluate(() => (window as typeof window & { __copiedInstall?: string }).__copiedInstall || '')).toContain('pk_live_activationkey000000')
  await capture(page, testInfo, 'activation-awaiting-connection')

  await expect(page.getByRole('heading', { name: 'Widget connected' })).toBeVisible({ timeout: 8_000 })
  expect(detailRequests).toBeGreaterThan(0)
  await capture(page, testInfo, 'activation-connected')
})
