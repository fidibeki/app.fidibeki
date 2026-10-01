import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { createHash, randomBytes, randomUUID } from 'node:crypto'

const databaseURL = process.env.SUPPORT_E2E_DATABASE_URL || ''
const app = new URL(process.env.SUPPORT_E2E_WEB_URL || 'http://support-e2e.localhost:4329')
app.hostname = 'website-chat-e2e.localhost'
function sql(statement: string) {
  const target = new URL(databaseURL)
  if (process.env.SUPPORT_E2E_ISOLATED !== '1' || !['127.0.0.1', 'localhost'].includes(target.hostname) || target.port !== '54339' || target.pathname !== '/fidibeki') throw new Error('Website chat requires the isolated loopback fixture database')
  return execFileSync('psql', [databaseURL, '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'], { input: statement, encoding: 'utf8' }).trim()
}

test('tenant talks to its own website visitor and feedback stays available', async ({ browser }) => {
  const tenant = randomUUID(), member = randomUUID(), project = randomUUID()
  const cookie = randomBytes(32).toString('base64url'), hash = createHash('sha256').update(cookie).digest('hex')
  const key = `pk_live_${randomBytes(11).toString('hex')}`
  sql(`INSERT INTO tenants(id,name,slug,plan,status) VALUES('${tenant}','Field Store','website-chat-e2e','growth','active');
    INSERT INTO tenant_members(id,tenant_id,email,name,status,permissions) VALUES('${member}','${tenant}','owner@shop.example','Alex at Field Store','active',ARRAY['chat:read','chat:write','chat:delete','project:read','project:update','feedback:read','issue:read','tenant:read']);
    UPDATE tenants SET owner_member_id='${member}' WHERE id='${tenant}';
    INSERT INTO tenant_member_sessions(tenant_id,member_id,refresh_hash) VALUES('${tenant}','${member}','${hash}');
    INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains) VALUES('${project}','${tenant}','Field Store website','${key}',ARRAY['shop.localhost']);
    INSERT INTO widget_rules(project_id,name,matcher_type,trigger_type,trigger_delay_seconds,localizations) VALUES('${project}','Website feedback','all_pages','delay',0,'{"en":{"launcher_label":"Feedback","prompt":"How was your visit?","comment_placeholder":"Tell us more","contact_label":"Email","consent_label":"Email me","submit_label":"Send feedback","success_message":"Your feedback was sent."}}');
    INSERT INTO widget_rule_target_conditions(widget_rule_id,kind,matcher_type,position) SELECT id,'include','all_pages',0 FROM widget_rules WHERE project_id='${project}';`)
  const operatorContext = await browser.newContext(), visitorContext = await browser.newContext()
  await operatorContext.addCookies([{ name: 'fidibeki_tenant_refresh', value: cookie, domain: app.hostname, path: '/', httpOnly: true, sameSite: 'Strict' }])
  const dashboard = await operatorContext.newPage(), site = await visitorContext.newPage()
  const errors: string[] = []
  dashboard.on('pageerror', error => errors.push(error.message)); site.on('pageerror', error => errors.push(error.message))
  try {
    await dashboard.goto(`${app.origin}/conversations?project=${project}`)
    await expect(dashboard.getByRole('heading', { name: 'Conversations', exact: true })).toBeVisible()
    await dashboard.getByRole('button', { name: 'Enable website chat', exact: true }).click()
    await expect(dashboard.getByRole('button', { name: 'Turn chat off', exact: true })).toBeVisible()
    await site.goto(`http://shop.localhost:4180/website-chat-fixture.html?key=${key}`)
    const widget = site.locator('#fidibeki-chat')
    await expect(widget.getByRole('button', { name: 'Messages & feedback', exact: true })).toBeVisible()
    await widget.getByRole('button', { name: 'Messages & feedback', exact: true }).click()
    await expect(widget.getByText('Your conversations live here.')).toBeVisible()
    await expect(widget.getByRole('textbox', { name: 'Your reply', exact: true })).toBeHidden()
    await widget.getByRole('button', { name: 'Close conversation', exact: true }).click()
    await expect(dashboard.locator('.chat-person')).toHaveCount(1)
    await dashboard.locator('.chat-person').click()
    await dashboard.getByRole('textbox', { name: /^Message to Visitor/ }).fill('Hi! Can I help you choose something?')
    await dashboard.getByRole('button', { name: 'Send message', exact: true }).click()
    await expect(widget.locator('.preview')).toContainText('Hi! Can I help you choose something?')
    await widget.locator('.preview-open').click()
    await widget.getByRole('textbox', { name: 'Your reply', exact: true }).fill('Yes, I have a question about delivery.')
    await widget.getByRole('button', { name: 'Send reply', exact: true }).click()
    await expect(dashboard.getByRole('log')).toContainText('Yes, I have a question about delivery.')
    await site.reload()
    await widget.getByRole('button', { name: 'Messages & feedback', exact: true }).click()
    await expect(widget.getByRole('log')).toContainText('Yes, I have a question about delivery.')
    const secondTab = await visitorContext.newPage()
    await secondTab.goto(site.url())
    await expect(secondTab.locator('#fidibeki-chat')).toBeVisible()
    await expect(dashboard.locator('.chat-person')).toHaveCount(1)
    await visitorContext.setOffline(true)
    await dashboard.getByRole('textbox', { name: /^Message to Visitor/ }).fill('Delivery takes two working days.')
    await dashboard.getByRole('button', { name: 'Send message', exact: true }).click()
    await visitorContext.setOffline(false)
    await expect(widget.getByRole('log')).toContainText('Delivery takes two working days.', { timeout: 30_000 })
    const downloadPromise = site.waitForEvent('download')
    await widget.getByRole('button', { name: 'Download conversation', exact: true }).click()
    const download = await downloadPromise
    expect(JSON.parse(readFileSync((await download.path())!, 'utf8')).messages).toHaveLength(3)
    await widget.getByRole('button', { name: 'Give feedback', exact: true }).click()
    await expect(widget.getByText('How was your visit?')).toBeVisible()
    await widget.getByRole('textbox', { name: 'Feedback', exact: true }).fill('This website conversation was helpful.')
    await widget.getByRole('button', { name: 'Send feedback', exact: true }).click()
    await expect(widget.getByText('Your feedback was sent.')).toBeVisible()
    expect(sql(`SELECT count(*) FROM feedbacks WHERE tenant_id='${tenant}' AND description='This website conversation was helpful.'`)).toBe('1')
    await widget.getByRole('button', { name: 'Messages', exact: true }).click()
    await site.screenshot({ path: '../docs/evidence/website-chat-visitor-desktop.png', animations: 'disabled' })
    await dashboard.screenshot({ path: '../docs/evidence/website-chat-dashboard-desktop.png', animations: 'disabled' })
    await dashboard.setViewportSize({ width: 390, height: 844 }); await site.setViewportSize({ width: 390, height: 844 })
    await expect(dashboard.getByRole('button', { name: 'Back to visitors' })).toBeVisible()
    for (const page of [site, dashboard]) expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await site.screenshot({ path: '../docs/evidence/website-chat-visitor-mobile.png', animations: 'disabled' })
    await dashboard.screenshot({ path: '../docs/evidence/website-chat-dashboard-mobile.png', animations: 'disabled' })
    await widget.getByRole('button', { name: 'Forget this chat on this browser', exact: true }).click()
    await expect(widget.getByText('Your conversations live here.')).toBeVisible()
    await dashboard.getByRole('button', { name: 'Delete conversation', exact: true }).click()
    await dashboard.getByRole('dialog').getByRole('button', { name: 'Delete conversation', exact: true }).click()
    await expect(dashboard.getByRole('dialog')).toBeHidden()
    await expect.poll(() => sql(`SELECT count(*) FROM website_chat_messages WHERE project_id='${project}';`)).toBe('0')
    const incoming = dashboard.getByRole('switch', { name: 'Allow visitors to start conversations', exact: true })
    await expect(incoming).not.toBeChecked()
    await incoming.click()
    await expect(incoming).toBeChecked()
    await expect(widget.getByRole('textbox', { name: 'Your message', exact: true })).toBeVisible()
    await dashboard.screenshot({ path: '../docs/evidence/website-chat-incoming-setting-mobile.png', animations: 'disabled' })
    await site.screenshot({ path: '../docs/evidence/website-chat-incoming-visitor-mobile.png', animations: 'disabled' })
    await widget.getByRole('textbox', { name: 'Your message', exact: true }).fill('Hello, I would like help with my order.')
    await widget.getByRole('button', { name: 'Send message', exact: true }).click()
    await dashboard.getByRole('button', { name: /^Inbox/ }).click()
    await expect(dashboard.locator('.chat-person')).toHaveCount(1)
    await expect(dashboard.locator('.chat-person')).toContainText('Hello, I would like help with my order.')
    await dashboard.locator('.chat-person').click()
    await expect(dashboard.getByRole('log')).toContainText('Hello, I would like help with my order.')
    await incoming.click()
    await expect(incoming).not.toBeChecked()
    await widget.getByRole('textbox', { name: 'Your reply', exact: true }).fill('The order number is 123.')
    await widget.getByRole('button', { name: 'Send reply', exact: true }).click()
    await expect(dashboard.getByRole('log')).toContainText('The order number is 123.')
    await widget.getByRole('button', { name: 'Forget this chat on this browser', exact: true }).click()
    await expect(widget.getByText('Your conversations live here.')).toBeVisible()
    await expect(widget.getByRole('textbox')).toBeHidden()
    expect(errors).toEqual([])
  } finally {
    await operatorContext.close(); await visitorContext.close()
    sql(`DELETE FROM tenants WHERE id='${tenant}';`)
  }
})
