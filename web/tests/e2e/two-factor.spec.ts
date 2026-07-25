import { expect, test, type Page, type Route } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const tenantOrigin = `http://acme.localhost:${port}`
const now = '2026-07-21T12:00:00.000Z'

const member = {
  id: 'member-1',
  tenant_id: 'tenant-1',
  email: 'owner@acme.test',
  name: 'Ada',
  locale: null,
  timezone: null,
  permissions: allTenantPermissions,
  status: 'active',
  role: 'owner',
  verified: true,
}

const tenant = {
  id: 'tenant-1',
  name: 'Acme',
  slug: 'acme',
  plan: 'growth',
  status: 'active',
  reporter_access_enabled: false,
}

const session = {
  access_token: 'access-token',
  token_type: 'Bearer',
  expires_in: 600,
  member,
  tenant,
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

/**
 * A minimal, tiny QR symbol. The encoder itself is covered by golden vectors in
 * the Go suite; here it only has to render.
 */
const qrCodeSVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4 4" role="img">'
  + '<rect width="100%" height="100%" fill="#ffffff"/>'
  + '<path fill="#000000" d="M1 1h1v1h-1z"/></svg>'

test('a pending challenge blocks the workspace until a code is accepted', async ({ page }) => {
  let challenged = false
  await route(page, async (route, path, method) => {
    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      // Primary authentication succeeded but the session still owes its
      // challenge, so no access token is returned.
      return challenged
        ? json(route, session)
        : json(route, { two_factor: { status: 'challenge_required' } })
    }
    if (path === '/auth/2fa/challenge' && method === 'POST') {
      const body = route.request().postDataJSON() as { code?: string }
      if (body?.code !== '123456') {
        return json(route, {
          error: { code: 'two_factor_code_invalid', message: 'two-factor code is invalid' },
        }, 401)
      }
      challenged = true
      return json(route, session)
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })

  await page.goto(`${tenantOrigin}/auth/verify#token=challenge-token`)
  await expect(page).toHaveURL(/\/auth\/two-factor/)
  await expect(page.getByRole('heading', { name: 'Enter your verification code' })).toBeVisible()

  await page.getByLabel('Authenticator code').fill('000000')
  await page.getByRole('button', { name: 'Verify and continue' }).click()
  await expect(page.getByRole('alert')).toContainText('not valid')

  // The recovery path is offered without leaving the page.
  await page.getByRole('button', { name: 'Use a recovery code instead' }).click()
  await expect(page.getByLabel('Recovery code')).toBeVisible()
  await page.getByRole('button', { name: 'Use my authenticator app instead' }).click()

  await page.getByLabel('Authenticator code').fill('123456')
  await page.getByRole('button', { name: 'Verify and continue' }).click()
  await expect(page).not.toHaveURL(/\/auth\/two-factor/)
})

test('a required policy routes an unenrolled member into setup', async ({ page }) => {
  let enrolled = false
  await route(page, async (route, path, method) => {
    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, enrolled
        ? session
        : { ...session, two_factor: { status: 'enrollment_required' } })
    }
    if (path === '/v1/projects' && method === 'GET') return json(route, { projects: [] })
    if (path === '/v1/security/two-factor' && method === 'GET') {
      return json(route, {
        two_factor: {
          policy: 'required',
          enrolled,
          confirmed_at: enrolled ? now : null,
          recovery_codes_remaining: enrolled ? 10 : 0,
          can_remove: false,
          authentication_method: 'local',
        },
      })
    }
    if (path === '/v1/security/two-factor/enroll' && method === 'POST') {
      return json(route, {
        enrollment: {
          secret: 'JBSWY3DPEHPK3PXP',
          provisioning_uri: 'otpauth://totp/Acme:owner@acme.test?secret=JBSWY3DPEHPK3PXP',
          qr_code_svg: qrCodeSVG,
        },
      }, 201)
    }
    if (path === '/v1/security/two-factor/confirm' && method === 'POST') {
      enrolled = true
      return json(route, { recovery_codes: ['ABCDE-FGHJK', 'KMNPQ-RSTVW'] })
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })

  await page.goto(`${tenantOrigin}/auth/verify#token=enrollment-token`)
  await expect(page).toHaveURL(/\/auth\/two-factor-setup/)
  await expect(page.getByText('Finish this setup to continue to your workspace.')).toBeVisible()

  await page.getByRole('button', { name: 'Set up authenticator app' }).click()
  // Both the scannable code and the manual key are offered.
  await expect(page.locator('.two-factor-qr svg')).toBeVisible()
  await expect(page.getByText('JBSWY3DPEHPK3PXP')).toBeVisible()

  await page.getByLabel('Authenticator code').fill('123456')
  await page.getByRole('button', { name: 'Confirm and enable' }).click()
  await expect(page.getByRole('heading', { name: 'Save your recovery codes' })).toBeVisible()
  await expect(page.getByText('ABCDE-FGHJK')).toBeVisible()
})

function route(
  page: Page,
  handler: (route: Route, path: string, method: string) => Promise<void>,
) {
  return page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    await handler(route, new URL(request.url()).pathname, request.method())
  })
}
