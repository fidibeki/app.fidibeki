import { expect, test } from '@playwright/test'
import { allTenantPermissions } from '../../data/permissions'
import { mockSupportRequest } from './support-fixture'

const origin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || '4321'}`
const now = new Date().toISOString()
const tenant = { id: 'tenant-1', name: 'Acme Studio', slug: 'acme', plan: 'growth', status: 'active', default_locale: 'en', default_timezone: 'UTC', widget_appearance_defaults: {}, logo_version: null }
const member = { id: 'member-chat', tenant_id: tenant.id, email: 'operator@example.test', name: 'Operator', permissions: [...allTenantPermissions], status: 'active', role: 'owner', verified: true, locale: 'en', timezone: 'UTC', avatar_version: null }
const conversation = (id: number) => ({ id: String(id), project_id: 'project-chat', name: `Visitor ${id}`, last_sequence: 100, operator_read_sequence: 100, visitor_read_sequence: 100, updated_at: now, expires_at: new Date(Date.now() + 86400000).toISOString(), unread: 0, preview: 'Hello' })
const message = (id: string, sequence: number) => ({ id: `${id}-${sequence}`, sequence, client_id: `${id}-${sequence}`, sender_type: 'operator', sender_name: 'Operator', body: `History ${id} message ${sequence}`, created_at: now })

test('inbox refresh preserves loaded pages and older history remains usable after switching visitors', async ({ page }) => {
  let firstPageRequests = 0, olderAStarted = false
  let finishOlderA!: () => void
  const olderA = new Promise<void>(resolve => { finishOlderA = resolve })
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async route => {
    const url = new URL(route.request().url()), path = url.pathname
    if (route.request().resourceType() === 'document') return route.continue()
    if (await mockSupportRequest(route)) return
    if (path === '/v1/config') return route.fulfill({ json: { tenant, plan: { plan: 'growth', limits: { projects: 4, feedback_per_month: 2500, users: 3, storage_days: 90 }, features: { ai: true, session_replay: true, slack: false, video: false, audio: true, dom_capture: true, custom_domain: false } } } })
    if (path === '/auth/refresh' || path === '/auth/verify') return route.fulfill({ json: { access_token: 'chat-test', token_type: 'Bearer', expires_in: 900, member, tenant } })
    if (path === '/v1/projects') return route.fulfill({ json: { projects: [{ id: 'project-chat', name: 'Acme store', tenant_id: tenant.id, public_key: 'pk_chat', allowed_domains: ['shop.test'], created_at: now, updated_at: now }] } })
    if (path.endsWith('/chat/settings')) return route.fulfill({ json: { enabled: true } })
    if (path.endsWith('/chat/events')) return route.fulfill({ contentType: 'text/event-stream', body: ': ready\n\n' })
    if (path.endsWith('/chat/online')) return route.fulfill({ json: { visitors: [], truncated: false } })
    if (path.endsWith('/chat/conversations')) {
      const before = url.searchParams.get('before')
      if (!before) firstPageRequests++
      return route.fulfill({ json: { conversations: before ? [conversation(51), conversation(52)] : Array.from({ length: 50 }, (_, index) => conversation(index + 1)), next_cursor: before ? '' : 'page-two' } })
    }
    const id = path.match(/\/chat\/visitors\/(\d+)$/)?.[1]
    if (id) {
      const before = url.searchParams.get('before')
      if (id === '51' && before) { olderAStarted = true; await olderA }
      return route.fulfill({ json: { conversation: conversation(Number(id)), messages: [message(id, before ? 99 : 100)], has_more: true } })
    }
    if (path.endsWith('/widget/config')) return route.fulfill({ json: { project_id: 'irrelevant', rules: [], features: {} } })
    return route.fulfill({ json: {} })
  })
  await page.goto(`${origin}/auth/verify?return_to=%2Fconversations#token=chat-test`)
  await expect(page.getByRole('heading', { name: 'Conversations', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Inbox', exact: true }).click()
  await expect(page.locator('.chat-person')).toHaveCount(50)
  await page.getByRole('button', { name: 'Load more conversations', exact: true }).click()
  await expect(page.locator('.chat-person')).toHaveCount(52)
  const previous = firstPageRequests
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await expect.poll(() => firstPageRequests).toBeGreaterThan(previous)
  await expect(page.locator('.chat-person')).toHaveCount(52)
  await page.locator('.chat-person').filter({ hasText: 'Visitor 51' }).click()
  const older = page.getByRole('button', { name: 'Load earlier messages', exact: true })
  await older.click()
  await expect.poll(() => olderAStarted).toBe(true)
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await page.locator('.chat-person').filter({ hasText: 'Visitor 52' }).click()
  await expect(older).toBeEnabled()
  await older.click()
  await expect(page.getByRole('log')).toContainText('History 52 message 99')
  finishOlderA()
  await expect(page.getByRole('log')).not.toContainText('History 51')
  await expect(older).toBeEnabled()
})
