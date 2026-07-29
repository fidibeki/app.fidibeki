import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const baseOrigin = `http://localhost:${port}`
const token = 'efi_1234567890123456789012345678901234567890123'

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

test('Portuguese email-feedback opt-out requires an explicit action and confirms it', async ({
  page,
}, testInfo) => {
  let postCount = 0
  await page.route(`**/v1/public/email-feedback/${token}/opt-out`, async (route) => {
    if (route.request().method() === 'POST') {
      postCount += 1
      expect(route.request().postDataJSON()).toEqual({})
      return json(route, { opted_out: true })
    }
    return json(route, {
      opt_out: {
        controller_name: 'Loja Acme',
        locale: 'pt-PT',
        privacy_policy_url: 'https://acme.example/privacy',
        opted_out: false,
      },
    })
  })

  await page.goto(`${baseOrigin}/email-feedback/${token}/opt-out`)
  await expect(page).toHaveTitle('Preferências de feedback por email - Fidibeki')
  await expect(page.getByRole('heading', {
    name: 'Deixar de receber pedidos de feedback',
  })).toBeVisible()
  await expect(page.getByText(/Loja Acme/)).toBeVisible()
  expect(postCount).toBe(0)
  await capture(page, testInfo, 'email-feedback-opt-out-pt')

  await page.getByRole('button', { name: 'Deixar de receber pedidos' }).click()
  await expect.poll(() => postCount).toBe(1)
  await expect(page.getByRole('heading', {
    name: 'Pedidos por email desativados',
  })).toBeVisible()
})

test('email-feedback opt-out renders completed, invalid, and mobile states safely', async ({
  page,
}, testInfo) => {
  await page.addInitScript(() => localStorage.setItem('fidibeki-theme', 'dark'))
  await page.route(`**/v1/public/email-feedback/${token}/opt-out`, route => json(route, {
    opt_out: {
      controller_name: 'Acme Research',
      locale: 'en',
      privacy_policy_url: 'https://acme.example/privacy',
      opted_out: true,
    },
  }))
  await page.setViewportSize({ width: 360, height: 720 })
  await page.goto(`${baseOrigin}/email-feedback/${token}/opt-out`)
  await expect(page.getByRole('heading', { name: 'Email requests stopped' })).toBeVisible()
  await expect(page.locator('.app-brand-dark')).toBeVisible()
  await expect(page.locator('.app-brand-light')).toBeHidden()
  await capture(page, testInfo, 'email-feedback-opt-out-complete-mobile')

  await page.unroute(`**/v1/public/email-feedback/${token}/opt-out`)
  await page.route('**/v1/public/email-feedback/*/opt-out', route => json(route, {
    error: { code: 'not_found', message: 'raw upstream detail' },
  }, 404))
  await page.goto(`${baseOrigin}/email-feedback/missing-token/opt-out`)
  await expect(page.getByRole('heading', { name: 'Link unavailable' })).toBeVisible()
  await expect(page.getByText('raw upstream detail')).toHaveCount(0)
})
