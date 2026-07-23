import { expect, test, type Page, type Route } from '@playwright/test'

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`

test.use({
  extraHTTPHeaders: {
    'Accept-Language': 'pt-AO,pt;q=0.9,en;q=0.7',
  },
  locale: 'pt-AO',
  timezoneId: 'Africa/Luanda',
})

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function runtimeSnapshot(page: Page) {
  return page.evaluate(() => {
    const root = document.querySelector('#__nuxt') as Element & {
      __vue_app__?: {
        config: {
          globalProperties: {
            $nuxt: {
              $i18n: {
                locale: { value: string } | string
                t: (key: string, ...arguments_: unknown[]) => string
              }
              payload: {
                state: Record<string, unknown>
              }
            }
          }
        }
      }
    }
    const nuxt = root.__vue_app__?.config.globalProperties.$nuxt
    if (!nuxt) throw new Error('Nuxt runtime is unavailable')
    const locale = typeof nuxt.$i18n.locale === 'string'
      ? nuxt.$i18n.locale
      : nuxt.$i18n.locale.value
    const regionalKey = Object.keys(nuxt.payload.state)
      .find(key => key.includes('regional:preferences'))
    return {
      locale,
      catalogLanguage: nuxt.$i18n.t('runtime.catalogLanguage'),
      fallbackProbe: nuxt.$i18n.t('runtime.fallbackProbe'),
      oneItem: nuxt.$i18n.t('runtime.itemCount', 1),
      manyItems: nuxt.$i18n.t('runtime.itemCount', 3),
      regional: regionalKey ? nuxt.payload.state[regionalKey] : undefined,
    }
  })
}

test('public SSR and hydration keep browser locale state aligned', async ({ page }) => {
  const hydrationMessages: string[] = []
  page.on('console', (message) => {
    if (/hydration/i.test(message.text())) hydrationMessages.push(message.text())
  })
  page.on('pageerror', (error) => {
    if (/hydration/i.test(error.message)) hydrationMessages.push(error.message)
  })

  const response = await page.goto(`${tenantOrigin}/login`)
  expect(response).not.toBeNull()
  const ssrHTML = await response!.text()
  expect(ssrHTML).toMatch(/<html[^>]*lang="pt-AO"/)
  expect(ssrHTML).toMatch(/<html[^>]*dir="ltr"/)
  await page.waitForFunction(() =>
    document.cookie.includes('fidibeki_browser_locale=pt-AO') &&
    document.cookie.includes('fidibeki_browser_timezone=Africa%2FLuanda'),
  )

  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-AO')
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr')
  expect(await runtimeSnapshot(page)).toMatchObject({
    locale: 'pt-AO',
    catalogLanguage: 'Português',
    fallbackProbe: 'English fallback',
    oneItem: 'um item',
    manyItems: '3 itens',
    regional: {
      locale: 'pt-AO',
      localeSource: 'browser',
      timezone: 'Africa/Luanda',
      timezoneSource: 'browser',
    },
  })
  expect(hydrationMessages).toEqual([])
})

test('member preferences override organization defaults and update reactively', async ({ page }) => {
  const tenant = {
    id: 'tenant-1',
    name: 'Acme Studio',
    slug: 'acme',
    plan: 'growth',
    status: 'active',
    reporter_access_enabled: true,
    default_locale: 'pt-AO',
    default_timezone: 'Africa/Luanda',
  }
  const member = {
    id: 'member-1',
    tenant_id: tenant.id,
    email: 'alex@acme.test',
    name: 'Alex Morgan',
    locale: 'pt-PT',
    timezone: 'Europe/Lisbon',
    permissions: [],
    status: 'active',
    role: 'member',
    verified: true,
  }
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const path = new URL(request.url()).pathname
    if (path === '/auth/verify' && request.method() === 'POST') {
      return json(route, {
        access_token: 'regional-access-token',
        token_type: 'Bearer',
        expires_in: 900,
        member,
        tenant,
      })
    }
    return json(route, { error: { code: 'not_found', message: 'Not found' } }, 404)
  })

  await page.goto(`${tenantOrigin}/auth/verify#token=regional-token`)
  await expect(page).toHaveURL(`${tenantOrigin}/access-denied`)
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT')
  expect(await runtimeSnapshot(page)).toMatchObject({
    locale: 'pt-PT',
    catalogLanguage: 'Português',
    regional: {
      locale: 'pt-PT',
      localeSource: 'member',
      timezone: 'Europe/Lisbon',
      timezoneSource: 'member',
    },
  })

  await page.evaluate(() => {
    const root = document.querySelector('#__nuxt') as Element & {
      __vue_app__?: {
        config: {
          globalProperties: {
            $nuxt: {
              payload: {
                state: Record<string, { member: { locale: string } }>
              }
            }
          }
        }
      }
    }
    const state = root.__vue_app__!.config.globalProperties.$nuxt.payload.state
    const authKey = Object.keys(state).find(key => key.includes('auth:session'))
    if (!authKey) throw new Error('Auth state is unavailable')
    state[authKey].member.locale = 'en-XA'
  })
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA')
  expect(await runtimeSnapshot(page)).toMatchObject({
    locale: 'en-XA',
    catalogLanguage: '[Ëñglïsh]',
    regional: {
      locale: 'en-XA',
      localeSource: 'member',
    },
  })
})
