import { expect, test, type Page, type Route } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const baseOrigin = `http://localhost:${port}`
const tenantOrigin = `http://acme.localhost:${port}`

const tenant = {
  id: 'tenant-theme',
  name: 'Acme Studio',
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
  id: 'member-theme',
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

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

// The theme lives entirely in the shell, so the dashboard only has to render —
// every payload below is the smallest shape that gets us an empty happy path.
async function installAPI(page: Page) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const path = new URL(request.url()).pathname

    if (path === '/auth/verify' || path === '/auth/refresh') {
      return json(route, {
        access_token: 'theme-token',
        token_type: 'Bearer',
        expires_in: 900,
        member,
        tenant,
      })
    }
    if (path === '/v1/dashboard') {
      return json(route, {
        dashboard: {
          greeting_name: 'Alex',
          totals: {},
          attention: [],
          work: [],
          projects: [],
        },
      })
    }
    if (path === '/v1/feedbacks') {
      return json(route, { feedbacks: [], pagination: { next_cursor: null, has_more: false, limit: 50 } })
    }
    if (path === '/v1/projects') {
      // Non-empty so /auth/verify routes to the dashboard shell rather than
      // the activation flow, which would need a full SSR load to reach.
      return json(route, {
        projects: [{
          id: 'project-1',
          tenant_id: tenant.id,
          name: 'Storefront',
          public_key: 'pk_live_storefront',
          allowed_domains: ['shop.acme.test'],
          default_locale: 'en',
          reporter_access_enabled: null,
          widget_appearance_overrides: {},
        }],
      })
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('system dark preference is respected by default', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' })
  const page = await context.newPage()
  await page.goto(`${baseOrigin}/login`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await context.close()
})

test('system light preference is respected by default', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'light' })
  const page = await context.newPage()
  await page.goto(`${baseOrigin}/login`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await context.close()
})

test('a stored choice overrides the system preference and survives navigation', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' })
  await context.addInitScript(() => {
    try {
      localStorage.setItem('fidibeki-theme', 'light')
    } catch {
      // ignore
    }
  })
  const page = await context.newPage()
  await page.goto(`${baseOrigin}/login`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

  await page.goto(`${baseOrigin}/signup`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await context.close()
})

test('theme is applied before first paint (no flash)', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'light' })
  await context.addInitScript(() => {
    try {
      localStorage.setItem('fidibeki-theme', 'dark')
    } catch {
      // ignore
    }
  })
  const page = await context.newPage()
  await page.goto(`${baseOrigin}/login`, { waitUntil: 'domcontentloaded' })
  // The inline head script must have applied the stored theme synchronously,
  // before hydration had any chance to run.
  const theme = await page.evaluate(() => document.documentElement.dataset.theme)
  expect(theme).toBe('dark')
  await context.close()
})

test('the shell toggle flips the theme and stores the choice', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'light' })
  const page = await context.newPage()
  await installAPI(page)

  await page.goto(`${tenantOrigin}/auth/verify#token=theme-token`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

  await page.getByRole('button', { name: 'Switch to dark theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  // Reload persistence is covered on /login; here we assert the write itself,
  // because reloading an app route needs a live upstream to survive auth.
  expect(await page.evaluate(() => localStorage.getItem('fidibeki-theme'))).toBe('dark')

  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  expect(await page.evaluate(() => localStorage.getItem('fidibeki-theme'))).toBe('light')
  await context.close()
})

test('an active workspace keeps theme and account controls right aligned', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'Desktop topbar alignment check')
  await installAPI(page)

  await page.goto(`${tenantOrigin}/auth/verify#token=theme-token`)
  const themeToggle = page.locator('.app-theme-toggle')
  const accountMenu = page.locator('.app-account-menu')
  await expect(themeToggle).toBeVisible()
  await expect(accountMenu).toBeVisible()
  await expect(page.locator('.app-trial-status')).toHaveCount(0)

  const controls = await page.evaluate(() => {
    const theme = document.querySelector('.app-theme-toggle')!.getBoundingClientRect()
    const account = document.querySelector('.app-account-menu')!.getBoundingClientRect()
    return {
      themeRight: theme.right,
      accountLeft: account.left,
      accountRight: account.right,
      viewportRight: document.documentElement.clientWidth,
    }
  })

  expect(controls.themeRight).toBeLessThanOrEqual(controls.accountLeft)
  expect(controls.viewportRight - controls.accountRight).toBeLessThanOrEqual(12)
})

test('both themes keep the primary button and page contrast intact', async ({ page }) => {
  await page.goto(`${baseOrigin}/login`)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((value) => {
      document.documentElement.dataset.theme = value
    }, theme)

    // Solid fills are theme-constant so --on-accent text keeps its ratio.
    const cta = page.locator('.auth-form .app-button--primary').first()
    const { background, color } = await cta.evaluate((element) => {
      const style = getComputedStyle(element)
      return { background: style.backgroundColor, color: style.color }
    })
    expect(background).toBe('rgb(8, 127, 115)')
    expect(color).toBe('rgb(255, 255, 255)')

    // Every token must resolve; an unset variable renders as transparent.
    const canvas = await page.evaluate(() =>
      getComputedStyle(document.body).backgroundColor,
    )
    expect(canvas).not.toBe('rgba(0, 0, 0, 0)')
  }

  const readCanvas = (theme: 'light' | 'dark') => page.evaluate((value) => {
    document.documentElement.dataset.theme = value
    return getComputedStyle(document.body).backgroundColor
  }, theme)

  // Sequential: both writes target the same attribute.
  const light = await readCanvas('light')
  const dark = await readCanvas('dark')
  expect(light).not.toBe(dark)
})
