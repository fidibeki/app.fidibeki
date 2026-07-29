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

test('rating link only preselects the localized form and POST is explicit', async ({
  page,
}, testInfo) => {
  let postCount = 0
  await page.route(`**/v1/public/email-feedback/${token}*`, async (route) => {
    if (route.request().method() === 'POST') {
      postCount += 1
      expect(route.request().postDataJSON()).toEqual({
        responses: {
          rating: 4,
          detail: 'O recibo devia estar mais visível.',
        },
        comment: 'O pagamento em si correu bem.',
        reporter_email: 'person@example.com',
        notify_reporter: true,
        consent: {
          purpose: 'feedback_updates',
          notice_version: '2026-07-controller-v1',
          controller_profile_version: 3,
          widget_definition_version: 7,
          locale: 'pt',
          granted: true,
        },
      })
      return json(route, { feedback_public_id: 'fb_AbCdEf12', submitted: true }, 201)
    }
    return json(route, {
      collection: {
        locale: 'pt',
        campaign_name: 'Acompanhamento da compra',
        introduction: 'Conte-nos como correu a sua compra.',
        response_blocks: [
          { key: 'rating', type: 'rating', required: true },
          { key: 'detail', type: 'free_text', required: true },
        ],
        allow_comment: true,
        collect_contact: true,
        ai_processing: { enabled: false },
        localization: {
          prompt: 'Como foi a sua experiência?',
          comment_placeholder: 'Mais algum contexto?',
          contact_label: 'O seu email',
          consent_label: 'Enviar-me atualizações',
          submit_label: 'Enviar feedback',
          success_message: 'Obrigado pelo seu feedback',
          privacy_purpose_summary: 'Enviar atualizações sobre este feedback.',
          privacy_lawful_basis_summary: 'Consentimento.',
          privacy_retention_summary: 'Até retirar o consentimento.',
          block_labels: {
            rating: 'Como avalia a experiência?',
            detail: 'O que aconteceu?',
          },
        },
        controller_name: 'Loja Acme',
        privacy_policy_url: 'https://acme.example/privacy',
        definition_version: 7,
        controller_profile_version: 3,
      },
    })
  })

  await page.goto(`${baseOrigin}/email-feedback/${token}?rating=4`)
  await expect(page).toHaveTitle('Como foi a sua experiência? - Fidibeki')
  await expect(page.getByRole('heading', { name: 'Como foi a sua experiência?' })).toBeVisible()
  await expect(page.getByRole('button', { name: '4 de 5' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  expect(postCount).toBe(0)

  await page.getByLabel('O que aconteceu?').fill('O recibo devia estar mais visível.')
  await page.getByLabel('Mais algum contexto?').fill('O pagamento em si correu bem.')
  await page.getByRole('checkbox', { name: 'Enviar-me atualizações' }).check()
  await page.getByLabel('O seu email').fill('person@example.com')
  await capture(page, testInfo, 'email-feedback-form-pt')
  await page.getByRole('button', { name: 'Enviar feedback' }).click()

  await expect.poll(() => postCount).toBe(1)
  await expect(page.getByRole('heading', {
    name: 'Obrigado pelo seu feedback',
  })).toBeVisible()
})

test('email feedback form handles unavailable and narrow mobile states safely', async ({
  page,
}, testInfo) => {
  await page.route('**/v1/public/email-feedback/*', route => json(route, {
    error: { code: 'not_found', message: 'raw upstream detail' },
  }, 404))
  await page.setViewportSize({ width: 360, height: 720 })
  await page.goto(`${baseOrigin}/email-feedback/missing-token?rating=5`)

  await expect(page.getByRole('heading', { name: 'Link unavailable' })).toBeVisible()
  await expect(page.getByText('raw upstream detail')).toHaveCount(0)
  await capture(page, testInfo, 'email-feedback-form-unavailable-mobile')
})
