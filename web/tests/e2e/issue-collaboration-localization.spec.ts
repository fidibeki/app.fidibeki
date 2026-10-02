import { expect, test, type Page, type Route, type TestInfo } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const tenantOrigin = `http://acme.localhost:${port}`
const nowDate = new Date()
const now = nowDate.toISOString()
const issueCreatedAt = new Date(nowDate.getTime() - 3 * 86_400_000).toISOString()

test.use({
  extraHTTPHeaders: {
    'Accept-Language': 'pt-AO,pt;q=0.9,en;q=0.7',
  },
  locale: 'pt-AO',
  timezoneId: 'Africa/Luanda',
})

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

const jamie = {
  id: 'member-2',
  tenant_id: tenant.id,
  email: 'jamie@acme.ao',
  name: 'Jamie Neto',
  avatar_version: null,
  locale: 'pt-AO',
  timezone: 'Africa/Luanda',
  permissions: [...allTenantPermissions],
  status: 'active',
  role: 'member',
  verified: true,
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

const customerTitle = 'Checkout button does nothing on Safari'
const customerDescription = 'Tapping "Pay now" on iOS Safari triggers no network request.'

function makeIssue() {
  return {
    id: 'issue-1',
    tenant_id: tenant.id,
    project_id: project.id,
    public_id: 'ISS-1042',
    title: customerTitle,
    description: customerDescription,
    status: 'open' as string,
    priority: 'high' as string | null,
    assignee: null as null | typeof jamie,
    tags: [] as Array<{ id: string, name: string, color: string }>,
    merged_into_id: undefined as string | undefined,
    reporter_email: 'client@example.com',
    notify_reporter: true,
    reporter_access_revoked_at: null as string | null,
    resolved_at: undefined as string | undefined,
    feedback_count: 1,
    created_at: issueCreatedAt,
    updated_at: issueCreatedAt,
  }
}

const otherIssue = {
  id: 'issue-2',
  tenant_id: tenant.id,
  project_id: project.id,
  public_id: 'ISS-1043',
  title: 'Duplicate report of the same Safari checkout issue',
  description: '',
  status: 'open',
  priority: null,
  assignee: null,
  tags: [],
  reporter_email: undefined,
  notify_reporter: false,
  reporter_access_revoked_at: null,
  feedback_count: 0,
  created_at: issueCreatedAt,
  updated_at: issueCreatedAt,
}

const tags = [
  { id: 'tag-1', name: 'ios', color: '#c0392b' },
  { id: 'tag-2', name: 'checkout', color: '#2471a3' },
]

const feedback = {
  id: 'feedback-9',
  public_id: 'FB-3301',
  project_id: project.id,
  project_name: project.name,
  rating: 1,
  title: 'App crashes right after tapping "Confirm order".',
  description: 'App crashes right after tapping "Confirm order".',
  text: 'App crashes right after tapping "Confirm order".',
  text_cleaned: 'App crashes right after tapping "Confirm order".',
  notify_reporter: false,
  page_url: 'https://shop.acme.ao/cart',
  status: 'open',
  ai_status: 'completed',
  priority: 'high',
  assignee: null,
  tags: [],
  sentiment_score: -0.8,
  moderation_flags: [],
  attachment_kinds: [],
  has_replay: false,
  created_at: issueCreatedAt,
  updated_at: now,
}

interface IssueActivity {
  id: string
  issue_id: string
  actor: { id: string, name?: string | null, email: string, avatar_version?: string | null } | null
  kind: string
  visibility: 'internal' | 'reporter'
  data: Record<string, unknown>
  created_at: string
}

interface IssueComment {
  id: string
  issue_id: string
  author: { id: string, name?: string | null, email: string, avatar_version?: string | null } | null
  body: string
  mentions: Array<{ id: string, name?: string | null, email: string, avatar_version?: string | null }>
  created_at: string
}

interface State {
  issue: ReturnType<typeof makeIssue>
  activities: IssueActivity[]
  comments: IssueComment[]
  lastPatchBody: Record<string, unknown> | null
  lastCommentBody: Record<string, unknown> | null
  lastReporterAccessBody: Record<string, unknown> | null
  lastMergeBody: Record<string, unknown> | null
  forceErrorOnce: { code: string } | null
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

function authResponse() {
  return {
    access_token: 'localized-issues-token',
    token_type: 'Bearer',
    expires_in: 900,
    member,
    tenant,
  }
}

function actorFrom(entity: typeof member | typeof jamie) {
  return { id: entity.id, name: entity.name, email: entity.email, avatar_version: entity.avatar_version }
}

async function installAPI(page: Page, state: State) {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request()
    if (request.resourceType() === 'document') return route.continue()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if ((path === '/auth/verify' || path === '/auth/refresh') && method === 'POST') {
      return json(route, authResponse())
    }
    if (path === '/v1/projects' && method === 'GET') {
      return json(route, { projects: [project] })
    }
    if (path === `/v1/projects/${project.id}/chat/issues/${state.issue.id}/conversations` && method === 'GET') {
      return json(route, { conversations: [], truncated: false })
    }
    if (path === '/v1/tags' && method === 'GET') {
      return json(route, { tags })
    }
    if (path === '/v1/members' && method === 'GET') {
      return json(route, { members: [jamie] })
    }
    if (path === '/v1/feedbacks' && method === 'GET') {
      return json(route, { feedbacks: [feedback], pagination: { has_more: false, limit: 20 } })
    }
    if (path === '/v1/issues' && method === 'GET') {
      return json(route, { issues: [state.issue, otherIssue] })
    }
    if (path === `/v1/issues/${state.issue.id}` && method === 'GET') {
      return json(route, {
        issue: state.issue,
        updates: [],
        comments: state.comments,
        activities: state.activities,
      })
    }
    if (path === `/v1/issues/${state.issue.id}` && method === 'PATCH') {
      const body = (request.postDataJSON() || {}) as Record<string, unknown>
      state.lastPatchBody = body
      if (state.forceErrorOnce) {
        const failure = state.forceErrorOnce
        state.forceErrorOnce = null
        return json(route, { error: { code: failure.code, message: 'raw upstream provider detail' } }, 503)
      }
      if ('status' in body) {
        const from = state.issue.status
        state.issue.status = body.status as string
        state.activities.push({
          id: `activity-${state.activities.length + 1}`,
          issue_id: state.issue.id,
          actor: actorFrom(member),
          kind: 'issue.status_changed',
          visibility: 'internal',
          data: { changes: { status: { from, to: body.status } }, transition_comment: body.comment || null },
          created_at: now,
        })
      } else if ('priority' in body) {
        state.issue.priority = body.priority === 'none' ? null : (body.priority as string)
      } else if ('assignee_id' in body) {
        state.issue.assignee = jamie.id === body.assignee_id ? jamie : null
      } else if ('unassign' in body) {
        state.issue.assignee = null
      } else if ('add_tag_ids' in body) {
        const tagID = (body.add_tag_ids as string[])[0]
        const tag = tags.find(candidate => candidate.id === tagID)
        if (tag && !state.issue.tags.some(existing => existing.id === tag.id)) state.issue.tags.push(tag)
      } else if ('remove_tag_ids' in body) {
        const tagID = (body.remove_tag_ids as string[])[0]
        state.issue.tags = state.issue.tags.filter(existing => existing.id !== tagID)
      } else if ('title' in body) {
        state.issue.title = body.title as string
        state.issue.description = body.description as string
      }
      return json(route, { issue: state.issue })
    }
    if (path === `/v1/issues/${state.issue.id}/reporter-access` && method === 'PATCH') {
      const body = (request.postDataJSON() || {}) as Record<string, unknown>
      state.lastReporterAccessBody = body
      state.issue.reporter_access_revoked_at = body.enabled ? null : now
      return json(route, { issue: state.issue })
    }
    if (path === `/v1/issues/${state.issue.id}/comments` && method === 'POST') {
      const body = (request.postDataJSON() || {}) as { body: string, mention_member_ids: string[] }
      state.lastCommentBody = body
      const mentions = (body.mention_member_ids || [])
        .map(id => [member, jamie].find(entity => entity.id === id))
        .filter((entity): entity is typeof jamie => Boolean(entity))
        .map(actorFrom)
      const comment: IssueComment = {
        id: `comment-${state.comments.length + 1}`,
        issue_id: state.issue.id,
        author: actorFrom(member),
        body: body.body,
        mentions,
        created_at: now,
      }
      const activity: IssueActivity = {
        id: `activity-${state.activities.length + 1}`,
        issue_id: state.issue.id,
        actor: actorFrom(member),
        kind: 'issue.comment_created',
        visibility: 'internal',
        data: {},
        created_at: now,
      }
      state.comments.push(comment)
      state.activities.push(activity)
      return json(route, { comment, activity })
    }
    if (path === `/v1/issues/${state.issue.id}/merge` && method === 'POST') {
      const body = (request.postDataJSON() || {}) as Record<string, unknown>
      state.lastMergeBody = body
      state.issue.merged_into_id = body.target_issue_id as string
      return json(route, {})
    }
    if (path === `/v1/issues/${state.issue.id}/merge` && method === 'DELETE') {
      state.issue.merged_into_id = undefined
      return json(route, {})
    }
    if (path === '/v1/notifications' && method === 'GET') {
      return json(route, {
        notifications: [{
          id: 'notification-1',
          kind: 'issue.mentioned',
          issue_id: state.issue.id,
          public_id: state.issue.public_id,
          issue_title: state.issue.title,
          actor: actorFrom(jamie),
          body: 'Can you take a look at this one?',
          read_at: null,
          created_at: now,
        }],
        unread_count: 1,
      })
    }
    if (path === '/v1/notifications/notification-1/read' && method === 'PATCH') {
      return json(route, {})
    }
    return json(route, { error: { code: 'not_found', message: 'unhandled route' } }, 404)
  })
}

test('Portuguese issue collaboration preserves customer content and exact mutation payloads', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const state: State = {
    issue: makeIssue(),
    comments: [
      { id: 'comment-1', issue_id: 'issue-1', author: actorFrom(member), body: 'Repro confirmed on my end too.', mentions: [], created_at: issueCreatedAt },
      { id: 'comment-2', issue_id: 'issue-1', author: null, body: 'Escalating to the mobile team.', mentions: [actorFrom(jamie)], created_at: issueCreatedAt },
    ],
    activities: [
      { id: 'activity-1', issue_id: 'issue-1', actor: actorFrom(member), kind: 'issue.created', visibility: 'internal', data: {}, created_at: issueCreatedAt },
    ],
    lastPatchBody: null,
    lastCommentBody: null,
    lastReporterAccessBody: null,
    lastMergeBody: null,
    forceErrorOnce: null,
  }
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify?return_to=%2Fissues#token=issues-token`)
  await expect(page.getByRole('heading', { name: 'Issues' })).toBeVisible()
  await expect(page).toHaveTitle('Issues · Fidibeki')
  await expect(page.getByText('Resolva o trabalho ligado a feedback e mantenha quem reportou informado.')).toBeVisible()
  await expect(page.getByPlaceholder('Pesquisar issues')).toBeVisible()
  await expect(page.getByText('2 issues')).toBeVisible()
  const issueRow = page.locator('.issues-row').filter({ hasText: customerTitle })
  await expect(issueRow).toContainText('Alta')
  await expect(issueRow).toContainText('3 dias')
  await capture(page, testInfo, 'issues-list-pt')

  await page.getByRole('link', { name: `Abrir ${state.issue.public_id}: ${customerTitle}` }).click()
  await expect(page).toHaveURL(new RegExp(`/issues/${state.issue.id}$`))
  await expect(page).toHaveTitle(`${state.issue.public_id} · Fidibeki`)
  await expect(page.getByRole('heading', { name: customerTitle })).toBeVisible()
  await expect(page.getByText(customerDescription)).toBeVisible()
  await expect(page.getByText('1 feedback · criada a')).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Dados da issue' })).toBeVisible()
  await expect(page.getByText('Quem reportou', { exact: true })).toBeVisible()
  await expect(page.getByText('client@example.com')).toBeVisible()
  await expect(page.locator('.status-pill').filter({ hasText: 'Aberta' })).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Comentários internos' })).toBeVisible()
  await expect(page.getByText('2 comentários')).toBeVisible()
  await expect(page.getByText('Repro confirmed on my end too.')).toBeVisible()
  await expect(page.getByText('Escalating to the mobile team.')).toBeVisible()
  await expect(page.getByText('Antigo membro')).toBeVisible()
  await expect(page.getByText('Mencionados: Jamie Neto')).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Feedback ligado' })).toBeVisible()
  await expect(page.getByText('App crashes right after tapping "Confirm order".')).toBeVisible()
  await expect(page.getByText('Concluído')).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Atividade' })).toBeVisible()
  await expect(page.getByText('1 entrada')).toBeVisible()
  await expect(page.getByText('Alex Morgan criou a issue')).toBeVisible()

  await page.getByRole('button', { name: 'Prioridade Urgente' }).click()
  await expect.poll(() => state.lastPatchBody).toEqual({ priority: 'urgent' })

  const assigneeSelect = page.getByLabel('Responsável', { exact: true })
  await assigneeSelect.selectOption(jamie.id)
  await expect.poll(() => state.lastPatchBody).toEqual({ assignee_id: jamie.id })

  await page.getByLabel('Etiqueta ios').check()
  await expect.poll(() => state.lastPatchBody).toEqual({ add_tag_ids: ['tag-1'] })

  const statusSelect = page.getByLabel('Estado da issue')
  await statusSelect.selectOption('resolved')
  await page.getByLabel('Contexto interno para esta mudança').fill('Following up with QA before closing.')
  await page.getByRole('button', { name: 'Atualizar issue' }).click()
  await expect.poll(() => state.lastPatchBody).toEqual({
    status: 'resolved',
    comment: 'Following up with QA before closing.',
    reporter_message: null,
  })
  await expect(page.getByText('2 entradas')).toBeVisible()
  await expect(page.getByText('Alex Morgan alterou o estado')).toBeVisible()
  await expect(page.getByText('estado: Aberta → Resolvida')).toBeVisible()
  await expect(page.getByText('Following up with QA before closing.')).toBeVisible()
  await expect(page.locator('.status-pill').filter({ hasText: 'Resolvida' })).toBeVisible()

  await page.getByLabel('Comentário interno').fill('Thanks for confirming, closing this out.')
  await page.getByLabel('Etiqueta checkout').check()
  await expect.poll(() => state.lastPatchBody).toEqual({ add_tag_ids: ['tag-2'] })
  await page.getByRole('checkbox', { name: 'Jamie Neto' }).check()
  await page.getByRole('button', { name: 'Adicionar comentário' }).click()
  await expect.poll(() => state.lastCommentBody).toEqual({
    body: 'Thanks for confirming, closing this out.',
    mention_member_ids: [jamie.id],
  })
  await expect(page.getByText('3 comentários')).toBeVisible()
  await expect(page.getByText('Thanks for confirming, closing this out.')).toBeVisible()

  await page.getByRole('button', { name: 'Revogar acesso ao progresso' }).click()
  await expect.poll(() => state.lastReporterAccessBody).toEqual({ enabled: false })
  await expect(page.getByRole('button', { name: 'Ativar acesso ao progresso' })).toBeVisible()
  await expect(page.getByText('Revogado')).toBeVisible()

  const mergeSelect = page.getByLabel('Issue de destino da fusão')
  await expect(mergeSelect.locator('option', { hasText: `${otherIssue.public_id} · ${otherIssue.title}` })).toHaveCount(1)
  await mergeSelect.selectOption(otherIssue.id)
  await expect(mergeSelect).toHaveValue(otherIssue.id)
  await page.getByRole('button', { name: 'Fundir' }).click()
  await expect.poll(() => state.lastMergeBody).toEqual({ target_issue_id: otherIssue.id })
  await expect(page.getByText('Esta issue está fundida noutra issue.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Anular fusão' })).toBeVisible()
  await capture(page, testInfo, 'issue-detail-pt')

  if (testInfo.project.name === 'mobile') {
    await page.getByRole('button', { name: 'Abrir navegação' }).click()
  }
  await page.getByRole('link', { name: 'Notificações', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Notificações' })).toBeVisible()
  await expect(page).toHaveTitle('Notificações · Fidibeki')
  await expect(page.getByText('1 por ler')).toBeVisible()
  await expect(page.getByText('Jamie Neto mencionou o seu nome')).toBeVisible()
  await expect(page.getByText('Can you take a look at this one?')).toBeVisible()
  await page.getByRole('button', { name: /Jamie Neto mencionou o seu nome/ }).click()
  await expect(page).toHaveURL(new RegExp(`/issues/${state.issue.id}$`))
})

test('Portuguese issue mutations surface safe localized errors without leaking provider detail', async ({ page }) => {
  test.setTimeout(60_000)
  const state: State = {
    issue: makeIssue(),
    comments: [],
    activities: [],
    lastPatchBody: null,
    lastCommentBody: null,
    lastReporterAccessBody: null,
    lastMergeBody: null,
    forceErrorOnce: { code: 'service_unavailable' },
  }
  await installAPI(page, state)

  await page.goto(`${tenantOrigin}/auth/verify?return_to=%2Fissues%2F${state.issue.id}#token=issues-error-token`)
  await expect(page.getByRole('heading', { name: customerTitle })).toBeVisible()

  await page.getByRole('button', { name: 'Prioridade Baixa' }).click()
  await expect(page.getByRole('alert')).toContainText('Este serviço está temporariamente indisponível.')
  await expect(page.getByRole('alert')).not.toContainText('raw upstream provider detail')

  state.forceErrorOnce = { code: 'not_a_known_code' }
  await page.getByRole('button', { name: 'Prioridade Baixa' }).click()
  await expect(page.getByRole('alert')).toContainText('Não foi possível atualizar a prioridade da issue.')
  await expect(page.getByRole('alert')).not.toContainText('raw upstream provider detail')
})
