import { expect, test, type Page, type Route } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const tenantOrigin = `http://loja.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = '2026-07-23T12:00:00.000Z'

test.use({
  extraHTTPHeaders: {
    'Accept-Language': 'pt-AO,pt;q=0.9,en;q=0.7',
  },
  locale: 'pt-AO',
  timezoneId: 'Africa/Luanda',
})

const tenant = {
  id: 'tenant-pt',
  name: 'Loja Angola',
  slug: 'loja',
  plan: 'growth',
  status: 'active',
  reporter_access_enabled: true,
  default_locale: 'en',
  default_timezone: 'UTC',
  privacy_profile_version: 1,
  privacy_contact_email: 'privacy@loja.ao',
  privacy_rights_email: 'direitos@loja.ao',
  privacy_policy_url: 'https://loja.ao/privacidade',
  widget_appearance_defaults: {},
  logo_version: null,
}

const member = {
  id: 'member-pt',
  tenant_id: tenant.id,
  email: 'ines@example.com',
  name: 'Inês Costa',
  locale: 'pt-AO',
  timezone: 'Africa/Luanda',
  permissions: [...allTenantPermissions],
  status: 'active',
  role: 'owner',
  verified: true,
  avatar_version: null,
}

const configuration = {
  tenant,
  plan: {
    plan: 'growth',
    monthly_price_cents: 4900,
    yearly_discount_percent: 15,
    limits: { projects: 10, feedback_per_month: 5000, users: 10, storage_days: 90 },
    features: {
      ai: true, session_replay: true, slack: true, video: false, audio: true,
      dom_capture: true, custom_domain: false,
    },
  },
}

// Customer-authored project. Its name and domain must never be translated.
const project = {
  id: 'project-1',
  tenant_id: tenant.id,
  name: 'Loja Central',
  public_key: 'pk_live_lojacentral',
  allowed_domains: ['loja.ao'],
  default_locale: 'pt-ao',
  reporter_access_enabled: null,
  widget_appearance_overrides: {},
  widget_last_seen_at: now,
  created_at: now,
  updated_at: now,
}

// Customer-authored widget rule and localization content. Must survive every mutation verbatim.
const rule = {
  id: 'rule-1',
  project_id: project.id,
  name: 'Promoção de Saída',
  priority: 1,
  enabled: true,
  matcher_type: 'url_prefix',
  matcher_value: 'https://loja.ao/checkout',
  include_conditions: [
    { id: 'cond-1', matcher_type: 'url_prefix', matcher_value: 'https://loja.ao/checkout', position: 0 },
  ],
  exclude_conditions: [],
  trigger_type: 'delay',
  trigger_delay_seconds: 5,
  trigger_scroll_percent: null,
  can_rate: true,
  can_write_text: true,
  can_record_audio: false,
  can_record_video: false,
  can_attach_element: false,
  can_attach_screenshot: false,
  greeting: 'Como foi a sua experiência de compra?',
  position: 'bottom-right',
  theme: 'auto',
  accent_color: '#087f73',
  replay_mode: 'none',
  replay_sampling_rate: 0,
  replay_max_duration_seconds: 1800,
  replay_max_events: 50000,
  replay_mask_all_inputs: true,
  replay_mask_text_selector: null,
  replay_record_canvas: false,
  replay_canvas_fps: 2,
  recurrence_mode: 'cooldown',
  recurrence_days: 30,
  definition_key: 'exit-promo',
  definition_version: 3,
  definition_status: 'active',
  presentation: 'prompt',
  objective: 'continuous_feedback',
  response_blocks: [
    { key: 'rating', type: 'rating', required: false },
    { key: 'feedback', type: 'free_text', required: false },
  ],
  localizations: {
    'pt-ao': {
      launcher_label: 'Feedback',
      prompt: 'Como foi a sua experiência de compra?',
      comment_placeholder: 'Conte-nos mais',
      contact_label: 'Endereço de email',
      consent_label: 'Contactem-me sobre este feedback',
      submit_label: 'Enviar feedback',
      success_message: 'O seu feedback foi enviado.',
      block_labels: {},
      option_labels: {},
    },
  },
  allow_comment: false,
  collect_contact: false,
  appearance_overrides: {},
}

interface Captured {
  createProjectBody?: { name: string, allowed_domains: string[] }
  createdProject?: typeof project
  updateProjectBody?: Record<string, unknown>
  projectOverride?: typeof project
  projectDeleted?: boolean
  createRuleBody?: Record<string, unknown>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  updateRuleBody?: Record<string, any>
  ruleOverride?: typeof rule
  ruleDeleted?: boolean
  saveRuleShouldFail?: boolean
  emailCampaigns?: Array<Record<string, unknown>>
  emailCampaignBody?: Record<string, unknown>
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

function authResponse() {
  return {
    access_token: 'localized-projects-token',
    token_type: 'Bearer',
    expires_in: 900,
    member,
    tenant,
  }
}

function resolveProject(state: Captured, id: string) {
  if (id === project.id) return state.projectOverride || project
  if (state.createdProject?.id === id) return state.createdProject
  return null
}

async function installAPI(page: Page, state: Captured) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, authResponse())
    }
    if (path === '/v1/config' && method === 'GET') {
      return json(route, configuration)
    }
    if (path === '/v1/dashboard' && method === 'GET') {
      return json(route, {
        dashboard: {
          range_days: 30, feedback_count: 0, open_issue_count: 0, attention_count: 0, trend: [],
        },
      })
    }
    if (path === '/v1/feedbacks' && method === 'GET') {
      return json(route, { feedbacks: [], pagination: { next_cursor: null, has_more: false, limit: 50 } })
    }
    if (path === '/v1/projects' && method === 'GET') {
      return json(route, { projects: [state.projectOverride || project] })
    }
    if (path === '/v1/projects' && method === 'POST') {
      state.createProjectBody = request.postDataJSON()
      state.createdProject = {
        ...project,
        id: 'project-2',
        public_key: 'pk_live_new',
        allowed_domains: [],
        ...state.createProjectBody,
      }
      return json(route, { project: state.createdProject })
    }
    if (path === '/v1/email-feedback-campaigns' && method === 'GET') {
      return json(route, { campaigns: state.emailCampaigns || [] })
    }
    if (path === '/v1/email-feedback-campaigns' && method === 'POST') {
      state.emailCampaignBody = request.postDataJSON()
      const campaign = {
        ...state.emailCampaignBody,
        id: 'campaign-1',
        tenant_id: tenant.id,
        widget_rule_name: rule.name,
        readiness: { ready: true, reasons: [] },
        created_at: now,
        updated_at: now,
      }
      state.emailCampaigns = [campaign]
      return json(route, { campaign }, 201)
    }
    const emailCampaignMatch = path.match(/^\/v1\/email-feedback-campaigns\/([^/]+)$/)
    if (emailCampaignMatch && method === 'PATCH') {
      state.emailCampaignBody = request.postDataJSON()
      const current = state.emailCampaigns?.find(candidate =>
        candidate.id === emailCampaignMatch[1])
      if (current) Object.assign(current, state.emailCampaignBody, { updated_at: now })
      return json(route, { campaign: current })
    }

    const projectMatch = path.match(/^\/v1\/projects\/([^/]+)$/)
    if (projectMatch && method === 'GET') {
      const target = resolveProject(state, projectMatch[1])
      if (!target) return json(route, { error: { code: 'not_found', message: 'unknown project' } }, 404)
      return json(route, { project: target })
    }
    if (projectMatch && projectMatch[1] === project.id && method === 'PATCH') {
      state.updateProjectBody = request.postDataJSON()
      state.projectOverride = { ...project, ...state.updateProjectBody }
      return json(route, { project: state.projectOverride })
    }
    if (projectMatch && projectMatch[1] === project.id && method === 'DELETE') {
      state.projectDeleted = true
      return json(route, {})
    }

    const rulesMatch = path.match(/^\/v1\/projects\/([^/]+)\/widget-rules$/)
    if (rulesMatch && method === 'GET') {
      if (rulesMatch[1] !== project.id) return json(route, { widget_rules: [] })
      return json(route, { widget_rules: state.ruleDeleted ? [] : [state.ruleOverride || rule] })
    }
    if (rulesMatch && rulesMatch[1] === project.id && method === 'POST') {
      state.createRuleBody = request.postDataJSON()
      return json(route, { widget_rule: { ...rule, id: 'rule-2', ...state.createRuleBody } })
    }

    const ruleMatch = path.match(/^\/v1\/projects\/([^/]+)\/widget-rules\/([^/]+)$/)
    if (ruleMatch && ruleMatch[1] === project.id && ruleMatch[2] === rule.id && method === 'PATCH') {
      if (state.saveRuleShouldFail) {
        return json(route, {
          error: { code: 'unexpected_widget_failure', message: 'internal provider trace xyz' },
        }, 500)
      }
      state.updateRuleBody = request.postDataJSON() as Captured['updateRuleBody']
      state.ruleOverride = { ...rule, ...state.updateRuleBody, id: rule.id }
      return json(route, { widget_rule: state.ruleOverride })
    }
    if (ruleMatch && ruleMatch[1] === project.id && ruleMatch[2] === rule.id && method === 'DELETE') {
      state.ruleDeleted = true
      return json(route, {})
    }

    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('project owner configures an opt-in email feedback campaign', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const state: Captured = {}
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=email-campaign-token`)
  await page.waitForURL(`${tenantOrigin}/`)
  await openProjects(page, testInfo)
  await page.getByRole('link', { name: 'Loja Central' }).click()
  await page.waitForURL(`${tenantOrigin}/projects/project-1`)

  const section = page.locator('#email-feedback-campaigns')
  await section.scrollIntoViewIfNeeded()
  await expect(section.getByRole('heading', { name: 'Feedback por email' })).toBeVisible()
  await expect(section).toContainText(
    'O email só é enviado quando o seu servidor chama a API de convites.',
  )
  await section.getByLabel('Nome da campanha').fill('Pós-compra')
  await section.getByLabel('Chave da campanha').fill('pos-compra')
  await section.getByLabel('Estado').selectOption('active')
  await section.getByLabel('Idioma predefinido').fill('fr')
  await expect(section).toContainText('A ativação está bloqueada')
  await expect(section).toContainText('A localização selecionada não existe')
  await section.getByLabel('Idioma predefinido').fill('pt-ao')

  await section.getByRole('button', { name: 'Guardar campanha' }).click()
  await expect(section.getByText('Campanha de email guardada.')).toBeVisible()
  await expect.poll(() => state.emailCampaignBody).toEqual({
    project_id: project.id,
    widget_rule_id: rule.id,
    campaign_key: 'pos-compra',
    name: 'Pós-compra',
    status: 'active',
    default_locale: 'pt-ao',
    subject: 'Como foi a sua experiência?',
    introduction: 'O seu feedback ajuda-nos a melhorar a experiência.',
    invitation_ttl_days: 30,
  })
  await expect(section).toContainText('Pronta para ativar')
  await expect(section).toContainText('email_feedback:send')
  await expect(section).toContainText('"campaign_id":"campaign-1"')
  if (testInfo.project.name === 'mobile') {
    const sectionBox = await section.boundingBox()
    const saveBox = await section.getByRole('button', { name: 'Guardar campanha' }).boundingBox()
    expect(sectionBox).not.toBeNull()
    expect(saveBox).not.toBeNull()
    expect(saveBox!.x).toBeGreaterThanOrEqual(sectionBox!.x)
    expect(saveBox!.x + saveBox!.width).toBeLessThanOrEqual(sectionBox!.x + sectionBox!.width + 1)
    expect(await section.locator('.email-campaign-layout > form').evaluate(
      element => element.scrollWidth <= element.clientWidth,
    )).toBe(true)
  }

  await section.getByLabel('Estado').selectOption('paused')
  await section.getByRole('button', { name: 'Guardar campanha' }).click()
  await expect.poll(() => state.emailCampaignBody?.status).toBe('paused')
  await expect(section.getByRole('button', { name: /Pós-compra/ })).toContainText('Pausada')
})

async function openProjects(page: Page, testInfo: { project: { name: string } }) {
  if (testInfo.project.name === 'mobile') {
    await page.getByRole('button', { name: 'Abrir navegação' }).click()
  }
  await page.getByRole('navigation', { name: 'Navegação principal' })
    .getByRole('link', { name: 'Projetos', exact: true })
    .click()
}

test('Portuguese project and widget rule editor preserve customer content and exact API payloads', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const state: Captured = {}
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=projects-token`)
  await page.waitForURL(`${tenantOrigin}/`)
  await openProjects(page, testInfo)
  await expect(page).toHaveTitle('Projetos · Fidibeki')
  await expect(page.getByRole('heading', { name: 'Projetos' })).toBeVisible()
  const projectRow = page.getByRole('link', { name: 'Loja Central' })
  await expect(projectRow).toBeVisible()
  await expect(projectRow).toContainText('loja.ao')

  await page.getByRole('button', { name: 'Novo projeto' }).click()
  const createDialog = page.getByRole('dialog', { name: 'Criar projeto' })
  await expect(createDialog).toBeVisible()
  await createDialog.getByLabel('Nome do projeto').fill('Loja Online Kianda')
  await createDialog.getByLabel('Domínios permitidos').fill('kianda.ao\n*.kianda.ao')
  await createDialog.getByRole('button', { name: 'Criar projeto', exact: true }).click()
  await expect.poll(() => state.createProjectBody).toEqual({
    name: 'Loja Online Kianda',
    allowed_domains: ['kianda.ao', '*.kianda.ao'],
  })

  await expect.poll(() => new URL(page.url()).pathname).toBe('/projects/project-2')
  await openProjects(page, testInfo)
  await page.waitForURL(`${tenantOrigin}/projects`)
  await expect(page).toHaveTitle('Projetos · Fidibeki')
  const lojaCentralLink = page.getByRole('link', { name: 'Loja Central' })
  await expect(lojaCentralLink).toBeVisible()
  await lojaCentralLink.click()
  await page.waitForURL(`${tenantOrigin}/projects/project-1`)
  await expect(page).toHaveTitle('Loja Central · Fidibeki')
  await expect(page.locator('a.app-button', { hasText: 'Projetos' })).toBeVisible()
  await expect(page.getByText('Configuração do projeto')).toBeVisible()
  await expect(page.getByText('1 regra de widget')).toBeVisible()
  await expect(page.getByText('Instalação')).toBeVisible()
  await expect(page.getByText('Não testado')).toBeVisible()
  await expect(page.getByText('Ativa · Prefixo de URL · prioridade 1')).toBeVisible()

  await expect(page.getByText('A editar regra')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Promoção de Saída', exact: true })).toBeVisible()

  const statusSelect = page.getByLabel('Estado do ciclo de vida')
  await expect(statusSelect).toHaveValue('active')
  await expect(statusSelect.locator('option[value="active"]')).toHaveText('Ativa')
  await expect(page.getByLabel('Nome', { exact: true })).toHaveValue('Promoção de Saída')
  await expect(page.getByLabel('Valor de inclusão 1')).toHaveValue('https://loja.ao/checkout')
  await expect(page.getByRole('textbox', { name: 'Pergunta', exact: true }))
    .toHaveValue('Como foi a sua experiência de compra?')
  await expect(page.getByRole('checkbox', { name: /^Áudio/ })).not.toBeChecked()
  await expect(page.getByRole('checkbox', { name: /^Captura de ecrã/ })).not.toBeChecked()

  await statusSelect.selectOption({ label: 'Rascunho' })

  const newPrompt = 'Adorou a compra? Conte-nos em 10 segundos.'
  await page.getByRole('textbox', { name: 'Pergunta', exact: true }).fill(newPrompt)

  await page.getByRole('button', { name: 'Guardar regra' }).click()
  await expect(page.getByText('Regra do widget guardada.')).toBeVisible()
  await expect.poll(() => state.updateRuleBody).toBeTruthy()
  expect(state.updateRuleBody!.definition_status).toBe('draft')
  expect(state.updateRuleBody!.name).toBe('Promoção de Saída')
  expect(state.updateRuleBody!.matcher_type).toBe('url_prefix')
  expect(state.updateRuleBody!.matcher_value).toBe('https://loja.ao/checkout')
  expect(state.updateRuleBody!.localizations['pt-ao'].prompt).toBe(newPrompt)
  expect(state.updateRuleBody!.greeting).toBe(newPrompt)
  expect(state.updateRuleBody!.localizations['pt-ao'].submit_label).toBe('Enviar feedback')

  await expect(page.getByText('Rascunho · Prefixo de URL · prioridade 1')).toBeVisible()

  state.saveRuleShouldFail = true
  await page.getByRole('button', { name: 'Guardar regra' }).click()
  await expect(page.getByRole('alert')).toContainText('Não foi possível guardar a regra do widget.')
  await expect(page.getByRole('alert')).not.toContainText('internal provider trace xyz')
  state.saveRuleShouldFail = false

  await page.getByRole('button', { name: 'Eliminar regra' }).click()
  const deleteDialog = page.getByRole('dialog', { name: 'Eliminar “Promoção de Saída”?' })
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByRole('button', { name: 'Cancelar' }).click()
  await expect(deleteDialog).not.toBeVisible()
  await expect(page.getByRole('heading', { name: 'Promoção de Saída', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Eliminar regra' }).click()
  await deleteDialog.getByRole('button', { name: 'Eliminar regra' }).click()
  await expect.poll(() => state.ruleDeleted).toBe(true)
  await expect(page.getByText('Nenhuma regra configurada.')).toBeVisible()

  await page.locator('#project-settings').scrollIntoViewIfNeeded()
  await expect(page.getByRole('heading', { name: 'Definições do projeto' })).toBeVisible()
  const newProjectName = 'Loja Central Renovada'
  await page.getByLabel('Nome do projeto').fill(newProjectName)
  await page.getByRole('button', { name: 'Guardar projeto' }).click()
  await expect(page.getByText('Definições do projeto guardadas.')).toBeVisible()
  await expect.poll(() => state.updateProjectBody).toBeTruthy()
  expect(state.updateProjectBody!.name).toBe(newProjectName)
  expect(state.updateProjectBody!.allowed_domains).toEqual(project.allowed_domains)
  expect(state.updateProjectBody!.default_locale).toBe(project.default_locale)

  await page.getByRole('button', { name: 'Eliminar projeto' }).click()
  const deleteProjectDialog = page.getByRole('dialog', { name: `Eliminar ${newProjectName}?` })
  await expect(deleteProjectDialog).toContainText(
    'Elimina permanentemente este projeto e o feedback, issues, anexos e dados de replay relacionados.',
  )
  await deleteProjectDialog.getByRole('button', { name: 'Eliminar projeto' }).click()
  await page.waitForURL(`${tenantOrigin}/projects`)
  expect(state.projectDeleted).toBe(true)
})

test('AI analysis requires an explicit governed decision and remains usable on mobile', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const state: Captured = {}
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify#token=ai-governance-token`)
  await page.waitForURL(`${tenantOrigin}/`)
  await openProjects(page, testInfo)
  await page.getByRole('link', { name: 'Loja Central' }).click()
  await page.waitForURL(`${tenantOrigin}/projects/project-1`)

  await page.getByRole('checkbox', { name: /^Ativar análise Gemini/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Ativar análise por AI' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('Processamento Google Gemini')).toBeVisible()
  await expect(dialog.getByText('As recomendações são apenas consultivas')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Ativar análise por AI' })).toBeDisabled()

  await dialog.getByLabel('URL do aviso de privacidade do cliente').fill('https://loja.ao/privacidade')
  await dialog.getByRole('checkbox', { name: /Confirmo que este workspace/ }).check()
  await expect(dialog.getByRole('button', { name: 'Ativar análise por AI' })).toBeEnabled()

  const box = await dialog.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width)

  await dialog.getByRole('button', { name: 'Ativar análise por AI' }).click()
  await expect(dialog).not.toBeVisible()
  await page.getByRole('button', { name: 'Guardar regra' }).click()
  await expect.poll(() => state.updateRuleBody?.ai_processing).toEqual({
    enabled: true,
    policy_version: '2026-07-24',
    provider: 'google_gemini',
    purposes: ['transcription', 'cleanup', 'moderation', 'sentiment', 'triage_recommendations'],
    input_categories: ['feedback_text'],
    privacy_policy_url: 'https://loja.ao/privacidade',
    sensitive_data_mode: 'prohibit_and_quarantine',
    acknowledged: true,
  })

  await page.getByRole('checkbox', { name: /^Ativar análise Gemini/ }).uncheck()
  await page.getByRole('button', { name: 'Guardar regra' }).click()
  await expect.poll(() => state.updateRuleBody?.ai_processing).toEqual({ enabled: false })
})
