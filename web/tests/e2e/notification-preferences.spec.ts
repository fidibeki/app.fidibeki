import { expect, test, type Page, type Route } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'
import { visibleLayoutDefects } from './layout-assertions'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const tenantOrigin = `http://acme.localhost:${port}`
const now = new Date().toISOString()
const windowStart = new Date(Date.now() - 86_400_000).toISOString()

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
  timezone: null,
  permissions: [...allTenantPermissions],
  status: 'active',
  role: 'owner',
  verified: true,
  avatar_version: null,
}

interface State {
  preferences: Record<string, unknown>
  lastPatchBody: Record<string, unknown> | null
}

function defaultPreferences() {
  return {
    assignments: 'immediate',
    mentions: 'immediate',
    comments: 'off',
    issue_changes: 'daily',
    feedback_summary: 'weekly',
    digest_hour: 8,
    digest_weekday: 1,
    timezone: 'UTC',
    timezone_source: 'default',
    updated_at: null,
  }
}

function preferencesResponse(state: State) {
  return {
    notification_preferences: state.preferences,
    categories: ['assignments', 'mentions', 'comments', 'issue_changes', 'feedback_summary'],
    supported_modes: {
      assignments: ['immediate', 'daily', 'weekly', 'off'],
      mentions: ['immediate', 'daily', 'weekly', 'off'],
      comments: ['immediate', 'daily', 'weekly', 'off'],
      issue_changes: ['immediate', 'daily', 'weekly', 'off'],
      feedback_summary: ['daily', 'weekly', 'off'],
    },
    deliveries: [
      {
        cadence: 'daily',
        status: 'sent',
        event_count: 3,
        window_start: windowStart,
        window_end: now,
        reason: null,
        sent_at: now,
        created_at: now,
      },
      {
        cadence: 'weekly',
        status: 'suppressed',
        event_count: 2,
        window_start: windowStart,
        window_end: now,
        reason: 'recipient address previously bounced',
        sent_at: null,
        created_at: now,
      },
    ],
  }
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function installAPI(page: Page, state: State) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const path = new URL(request.url()).pathname
    const method = request.method()

    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, {
        access_token: 'notification-token',
        token_type: 'Bearer',
        expires_in: 900,
        member,
        tenant,
      })
    }
    if (path === '/v1/notification-preferences' && method === 'GET') {
      return json(route, preferencesResponse(state))
    }
    if (path === '/v1/notification-preferences' && method === 'PATCH') {
      const body = (request.postDataJSON() || {}) as Record<string, unknown>
      state.lastPatchBody = body
      state.preferences = {
        ...state.preferences,
        ...body,
        timezone: body.timezone || 'UTC',
        timezone_source: body.timezone ? 'member' : 'default',
      }
      return json(route, preferencesResponse(state))
    }
    if (path === '/v1/notifications' && method === 'GET') {
      return json(route, { notifications: [], unread_count: 0 })
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('a member routes optional notifications and sees delivery diagnostics', async ({ page }) => {
  const state: State = { preferences: defaultPreferences(), lastPatchBody: null }
  await installAPI(page, state)

  await page.goto(
    `${tenantOrigin}/auth/verify?return_to=%2Fsettings%2Fnotifications#token=notification-token`,
  )
  await expect(page.getByRole('heading', { name: 'Notification preferences' })).toBeVisible()

  // The defaults must arrive low-noise, and a per-submission feedback email must
  // not even be offered as a choice.
  await expect(page.getByLabel('Assigned to you')).toHaveValue('immediate')
  await expect(page.getByLabel('Comments')).toHaveValue('off')
  await expect(page.getByLabel('New feedback summary')).toHaveValue('weekly')
  const summaryModes = await page.getByLabel('New feedback summary').locator('option')
    .evaluateAll(options => options.map(option => (option as HTMLOptionElement).value))
  expect(summaryModes).not.toContain('immediate')

  await expect(page.getByText(
    'Sign-in, security, account deletion, and billing emails are always sent and cannot be turned off here.',
  )).toBeVisible()

  await expect(page.getByText('recipient address previously bounced')).toBeVisible()
  await expect(page.getByText('Withheld', { exact: true })).toBeVisible()
  await expect(page.getByText('Sent', { exact: true })).toBeVisible()

  await page.getByLabel('Comments').selectOption('daily')
  await page.getByLabel('Use the workspace timezone').uncheck()
  await page.getByLabel('Timezone', { exact: true }).fill('Africa/Luanda')
  await page.getByLabel('Daily digest time').selectOption('18')
  await page.getByRole('button', { name: 'Save preferences' }).click()

  await expect(page.getByText('Notification preferences saved.')).toBeVisible()
  expect(state.lastPatchBody).toEqual({
    assignments: 'immediate',
    mentions: 'immediate',
    comments: 'daily',
    issue_changes: 'daily',
    feedback_summary: 'weekly',
    digest_hour: 18,
    digest_weekday: 1,
    timezone: 'Africa/Luanda',
  })
  expect(state.lastPatchBody).not.toHaveProperty('billing')

  expect(await visibleLayoutDefects(page)).toEqual([])
})

test('the weekly digest day only applies when a weekly digest is selected', async ({ page }) => {
  const state: State = {
    preferences: { ...defaultPreferences(), feedback_summary: 'off', issue_changes: 'immediate' },
    lastPatchBody: null,
  }
  await installAPI(page, state)

  await page.goto(
    `${tenantOrigin}/auth/verify?return_to=%2Fsettings%2Fnotifications#token=notification-token`,
  )
  await expect(page.getByRole('heading', { name: 'Notification preferences' })).toBeVisible()
  await expect(page.getByLabel('Daily digest time')).toBeDisabled()
  await expect(page.getByLabel('Weekly digest day')).toBeDisabled()

  await page.getByLabel('Important issue changes').selectOption('weekly')
  await expect(page.getByLabel('Daily digest time')).toBeEnabled()
  await expect(page.getByLabel('Weekly digest day')).toBeEnabled()
})
