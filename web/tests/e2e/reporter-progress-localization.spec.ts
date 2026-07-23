import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const baseOrigin = `http://localhost:${port}`
const nowDate = new Date()
const now = nowDate.toISOString()
const createdAt = new Date(nowDate.getTime() - 2 * 86_400_000).toISOString()

test.use({
  extraHTTPHeaders: {
    'Accept-Language': 'pt-AO,pt;q=0.9,en;q=0.7',
  },
  locale: 'pt-AO',
  timezoneId: 'Africa/Luanda',
})

const clarificationPrompt = 'Can you confirm your Safari version and iOS build number?'

function progressFixture() {
  return {
    public_id: 'ISS-1042',
    title: 'Checkout button does nothing on Safari',
    status: 'needs_info',
    created_at: createdAt,
    resolved_at: null,
    events: [
      { from_status: 'open', to_status: 'needs_info', created_at: now },
    ],
    clarification: {
      prompt: clarificationPrompt,
      responded: false,
      responded_at: null,
    },
  }
}

interface State {
  progress: ReturnType<typeof progressFixture>
  lastResponseBody: Record<string, unknown> | null
  forceResponseError: { code: string } | null
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

async function installAPI(page: Page, state: State, token: string) {
  await page.route(/^https?:\/\/[^/]+\/v1\/reporter\//, async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if (path === `/v1/reporter/progress/${token}` && method === 'GET') {
      return json(route, { progress: state.progress })
    }
    if (path === `/v1/reporter/progress/${token}/response` && method === 'POST') {
      if (state.forceResponseError) {
        const failure = state.forceResponseError
        state.forceResponseError = null
        return json(route, { error: { code: failure.code, message: 'raw upstream provider detail' } }, 503)
      }
      const body = (request.postDataJSON() || {}) as Record<string, unknown>
      state.lastResponseBody = body
      state.progress.clarification!.responded = true
      return json(route, {})
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('Portuguese reporter progress page preserves the clarification text and sends exact response payloads', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const token = 'reporter-token-pt'
  const state: State = { progress: progressFixture(), lastResponseBody: null, forceResponseError: null }
  await installAPI(page, state, token)

  await page.goto(`${baseOrigin}/reporter/${token}`)
  await expect(page).toHaveTitle(`Progresso de ${state.progress.public_id} - Fidibeki`)
  await expect(page.getByText(state.progress.public_id)).toBeVisible()
  await expect(page.getByRole('heading', { name: state.progress.title, level: 1 })).toBeVisible()
  await expect(page.getByText('Precisa de informação', { exact: true })).toBeVisible()
  await expect(page.getByText(/^Reportado a /)).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Pergunta da equipa' })).toBeVisible()
  await expect(page.getByText(clarificationPrompt)).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Progresso', exact: true })).toBeVisible()
  await expect(page.getByText('Feedback recebido')).toBeVisible()
  await expect(page.getByText('Estado alterado para Precisa de informação')).toBeVisible()
  await capture(page, testInfo, 'reporter-progress-pt')

  const responseText = 'iOS 17.5, Safari 17.4, still failing on the same device.'
  await page.getByLabel('A sua resposta').fill(responseText)
  await page.getByRole('button', { name: 'Enviar resposta' }).click()
  await expect.poll(() => state.lastResponseBody).toEqual({ response: responseText })
  await expect(page.getByText('Resposta enviada.')).toBeVisible()
  await expect(page.getByLabel('A sua resposta')).toHaveCount(0)
})

test('Portuguese reporter progress page surfaces safe localized errors for failed responses and invalid links', async ({ page }) => {
  test.setTimeout(60_000)
  const token = 'reporter-token-error'
  const state: State = { progress: progressFixture(), lastResponseBody: null, forceResponseError: { code: 'not_a_known_code' } }
  await installAPI(page, state, token)

  await page.goto(`${baseOrigin}/reporter/${token}`)
  await page.getByLabel('A sua resposta').fill('Trying again from an iPhone 13.')
  await page.getByRole('button', { name: 'Enviar resposta' }).click()
  await expect(page.getByRole('alert')).toContainText('Não foi possível enviar a sua resposta.')
  await expect(page.getByRole('alert')).not.toContainText('raw upstream provider detail')

  await page.route(/^https?:\/\/[^/]+\/v1\/reporter\//, async (route) => {
    await json(route, { error: { code: 'not_found', message: 'raw upstream provider detail' } }, 404)
  })
  await page.goto(`${baseOrigin}/reporter/missing-token`)
  await expect(page.getByRole('heading', { name: 'Progresso indisponível' })).toBeVisible()
  await expect(page.getByText('Esta ligação é inválida, expirou ou já não está disponível.')).toBeVisible()
  await expect(page.getByText('raw upstream provider detail')).toHaveCount(0)
})
