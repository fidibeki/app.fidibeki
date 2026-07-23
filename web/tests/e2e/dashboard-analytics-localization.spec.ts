import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = '2026-07-23T12:00:00.000Z'

test.use({
  extraHTTPHeaders: {
    'Accept-Language': 'pt-AO,pt;q=0.9,en;q=0.7',
  },
  locale: 'pt-AO',
  timezoneId: 'Africa/Luanda',
})

type Mode = 'data' | 'empty' | 'error'

interface LocalizedState {
  dashboardMode: Mode
  analyticsMode: Mode
  dashboardDelayMs: number
  analyticsDelayMs: number
  analyticsRequests: string[]
}

const tenant = {
  id: 'tenant-pt',
  name: 'Acme Angola',
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
  id: 'member-pt',
  tenant_id: tenant.id,
  email: 'alex@acme.ao',
  name: 'Alex Morgan',
  locale: 'pt-AO',
  timezone: 'Africa/Luanda',
  permissions: [...allTenantPermissions],
  status: 'active',
  role: 'owner',
  verified: true,
  avatar_version: null,
}

const project = {
  id: 'project-1',
  tenant_id: tenant.id,
  name: 'Storefront Angola',
  public_key: 'pk_live_storefront',
  allowed_domains: ['shop.acme.ao'],
  default_locale: 'pt-AO',
  reporter_access_enabled: null,
  widget_appearance_overrides: {},
  widget_last_seen_at: now,
  created_at: now,
  updated_at: now,
}

const feedback = {
  id: 'feedback-1',
  public_id: 'FB-2048',
  project_id: project.id,
  project_name: project.name,
  rating: 2,
  title: 'Checkout froze after I applied a promotion code.',
  description: 'Checkout froze after I applied a promotion code.',
  text: 'Checkout froze after I applied a promotion code.',
  text_cleaned: 'Checkout froze after I applied a promotion code.',
  notify_reporter: false,
  page_url: 'https://shop.acme.ao/checkout',
  status: 'open',
  ai_status: 'completed',
  priority: 'high',
  assignee: null,
  tags: [],
  sentiment_score: -0.74,
  moderation_flags: [],
  attachment_kinds: [],
  has_replay: false,
  created_at: '2026-07-21T10:00:00.000Z',
  updated_at: now,
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({
    path: testInfo.outputPath(`${name}.png`),
    fullPage: false,
    animations: 'disabled',
  })
}

function authResponse() {
  return {
    access_token: 'localized-dashboard-token',
    token_type: 'Bearer',
    expires_in: 900,
    member,
    tenant,
  }
}

function dashboardResponse(empty: boolean) {
  return {
    dashboard: {
      range_days: 30,
      feedback_count: empty ? 0 : 128,
      feedback_delta_percent: empty ? undefined : 12.4,
      open_issue_count: empty ? 0 : 2,
      satisfaction: empty ? null : 4.2,
      satisfaction_delta: empty ? undefined : 0.3,
      attention_count: empty ? 0 : 2,
      trend: empty
        ? []
        : [
            { date: '2026-07-19', feedback_count: 8, satisfaction: 4.3 },
            { date: '2026-07-20', feedback_count: 17, satisfaction: 4.2 },
            { date: '2026-07-21', feedback_count: 21, satisfaction: 4.4 },
          ],
    },
  }
}

function analyticsResponse(mode: Exclude<Mode, 'error'>, url: URL) {
  const empty = mode === 'empty'
  const range = url.searchParams.get('range') || '30d'
  const rangeDays = Number(range.slice(0, -1))
  const dates = Array.from({ length: rangeDays }, (_, index) => {
    const date = new Date('2026-07-21T00:00:00.000Z')
    date.setUTCDate(date.getUTCDate() - (rangeDays - index - 1))
    return date.toISOString().slice(0, 10)
  })
  const summary = empty
    ? {
        matched: 0,
        impressions: 0,
        interactions: 0,
        submissions: 0,
        dismissals: 0,
        impression_rate: null,
        interaction_rate: null,
        completion_rate: null,
        submit_rate: null,
      }
    : {
        matched: 1840,
        impressions: 1240,
        interactions: 496,
        submissions: 186,
        dismissals: 94,
        impression_rate: 0.674,
        interaction_rate: 0.4,
        completion_rate: 0.375,
        submit_rate: 0.15,
      }
  const row = <T extends Record<string, unknown>>(values: T) => ({ ...summary, ...values })
  const projects = empty ? [] : [row({ project_id: project.id, project_name: project.name })]
  const rules = empty
    ? []
    : [row({
        rule_id: 'rule-1',
        rule_name: 'Checkout pulse',
        project_id: project.id,
        project_name: project.name,
        deleted: false,
      })]
  const pages = empty
    ? []
    : [
        row({ page_url: 'https://shop.acme.ao/checkout' }),
        row({
          page_url: 'https://shop.acme.ao/checkout/success',
          impressions: 280,
          interactions: 168,
          submissions: 126,
          interaction_rate: 0.6,
          completion_rate: 0.75,
          submit_rate: 0.45,
        }),
      ]
  return {
    range,
    period: {
      start_date: dates[0],
      end_date: dates.at(-1),
      timezone: 'UTC',
    },
    filters: {},
    summary,
    trend: dates.map((date, index) => ({
      date,
      impressions: empty ? 0 : 22 + (index % 6) * 9,
      interactions: empty ? 0 : 8 + (index % 5) * 4,
      submissions: empty ? 0 : 2 + (index % 4) * 2,
    })),
    projects,
    rules,
    pages,
    options: {
      projects: [{ id: project.id, name: project.name }],
      rules: [{
        id: 'rule-1',
        project_id: project.id,
        name: 'Checkout pulse',
        deleted: false,
      }],
      pages: [
        { url: 'https://shop.acme.ao/checkout' },
        { url: 'https://shop.acme.ao/checkout/success' },
      ],
    },
    metadata: {
      projects: {
        total: projects.length,
        returned: projects.length,
        limit: 100,
        truncated: false,
      },
      rules: {
        total: rules.length,
        returned: rules.length,
        limit: 100,
        truncated: false,
      },
      pages: {
        total: empty ? 0 : 210,
        returned: pages.length,
        limit: 100,
        truncated: !empty,
      },
      project_options: { total: 1, returned: 1, limit: 200, truncated: false },
      rule_options: { total: 1, returned: 1, limit: 200, truncated: false },
      page_options: { total: 2, returned: 2, limit: 200, truncated: false },
    },
  }
}

async function installAPI(page: Page, state: LocalizedState) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, authResponse())
    }
    if (path === '/v1/dashboard' && method === 'GET') {
      if (state.dashboardDelayMs) {
        await new Promise(resolve => setTimeout(resolve, state.dashboardDelayMs))
      }
      if (state.dashboardMode === 'error') {
        return json(route, {
          error: {
            code: 'service_unavailable',
            message: 'private dashboard provider detail',
          },
        }, 503)
      }
      return json(route, dashboardResponse(state.dashboardMode === 'empty'))
    }
    if (path === '/v1/feedbacks' && method === 'GET') {
      return json(route, {
        feedbacks: state.dashboardMode === 'empty' ? [] : [feedback],
        pagination: { next_cursor: null, has_more: false, limit: 50 },
      })
    }
    if (path === '/v1/projects' && method === 'GET') {
      return json(route, { projects: state.dashboardMode === 'empty' ? [] : [project] })
    }
    if (path === '/v1/analytics/widget' && method === 'GET') {
      state.analyticsRequests.push(url.search)
      if (state.analyticsDelayMs) {
        await new Promise(resolve => setTimeout(resolve, state.analyticsDelayMs))
      }
      if (state.analyticsMode === 'error') {
        return json(route, {
          error: {
            code: 'service_unavailable',
            message: 'private analytics provider detail',
          },
        }, 503)
      }
      return json(route, analyticsResponse(state.analyticsMode, url))
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('Portuguese dashboard and analytics preserve calculations and operational states', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const state: LocalizedState = {
    dashboardMode: 'data',
    analyticsMode: 'data',
    dashboardDelayMs: 0,
    analyticsDelayMs: 500,
    analyticsRequests: [],
  }
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=dashboard-analytics-token`)
  await expect(page.getByRole('heading', { name: 'Boa tarde, Alex' })).toBeVisible()
  await expect(page).toHaveTitle('Visão geral · Fidibeki')
  await expect(page.getByText('2 feedbacks precisam de atenção.')).toBeVisible()
  await expect(page.locator('.dashboard-ledger article').filter({ hasText: 'Feedback recebido' })).toContainText('128')
  await expect(page.locator('.dashboard-ledger article').filter({ hasText: 'Feedback recebido' })).toContainText('+12,4%')
  await expect(page.locator('.dashboard-ledger article').filter({ hasText: 'Satisfação' })).toContainText('4,2')
  await expect(page.getByText('Checkout froze after I applied a promotion code.')).toBeVisible()
  await expect(page.locator('.dashboard-work-row').getByText('Sem issue', { exact: true })).toHaveText('Sem issue')
  await capture(page, testInfo, 'dashboard-pt')

  state.dashboardDelayMs = 500
  await page.getByLabel('Período do dashboard').selectOption('90d')
  await expect(page.getByRole('heading', { name: 'A carregar sinais do workspace' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Boa tarde, Alex' })).toBeVisible()

  state.dashboardMode = 'error'
  state.dashboardDelayMs = 0
  await page.getByLabel('Período do dashboard').selectOption('7d')
  await expect(page.getByRole('heading', { name: 'Dashboard indisponível' })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('Este serviço está temporariamente indisponível.')
  await expect(page.getByRole('alert')).not.toContainText('private dashboard provider detail')
  state.dashboardMode = 'data'
  await page.getByRole('button', { name: 'Tentar novamente' }).click()
  await expect(page.getByRole('heading', { name: 'Boa tarde, Alex' })).toBeVisible()

  if (testInfo.project.name === 'mobile') {
    await page.getByRole('button', { name: 'Abrir navegação' }).click()
  }
  await page.getByRole('link', { name: 'Análises', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'A carregar analytics' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Analytics', exact: true })).toBeVisible()
  await expect(page).toHaveTitle('Analytics · Fidibeki')
  await expect(page.locator('.analytics-heading')).toContainText('22/06/2026 – 21/07/2026')
  const impressions = page.locator('.analytics-ledger article').filter({ hasText: 'Impressões' })
  await expect(impressions).toContainText(/1[\s\u00a0]240/)
  const interactions = page.locator('.analytics-ledger article').filter({ hasText: 'Taxa de interação' })
  await expect(interactions).toContainText('40%')
  await expect(interactions).toContainText(/496 interações/)
  await expect(page.getByLabel('Séries da tendência')).toContainText('Submissões')
  await expect(page.getByRole('img', { name: 'Tendência diária do funil do widget' })).toBeVisible()
  await page.getByRole('tab', { name: 'Páginas' }).click()
  await expect(page.locator('.analytics-breakdown > header')).toContainText('2 de 210 páginas')
  await expect(page.getByRole('tabpanel')).toContainText('https://shop.acme.ao/checkout')
  await expect(page.getByRole('tabpanel')).toContainText('45%')
  await capture(page, testInfo, 'analytics-pt')

  state.analyticsMode = 'error'
  state.analyticsDelayMs = 0
  await page.getByRole('button', { name: '7d' }).click()
  await expect(page.getByRole('heading', { name: 'Analytics indisponível' })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('Este serviço está temporariamente indisponível.')
  await expect(page.getByRole('alert')).not.toContainText('private analytics provider detail')

  state.analyticsMode = 'empty'
  await page.getByRole('button', { name: 'Tentar novamente' }).click()
  await expect(page.getByText('Sem atividade do widget neste período')).toBeVisible()
  await expect(page.locator('.analytics-ledger article').filter({ hasText: 'Taxa de interação' })).toContainText('—')
  expect(state.analyticsRequests.at(-1)).toContain('range=7d')
})
