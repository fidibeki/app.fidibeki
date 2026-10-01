import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { createHash, randomBytes, randomUUID } from 'node:crypto'

const databaseURL = process.env.SUPPORT_E2E_DATABASE_URL || ''
const webURL = process.env.SUPPORT_E2E_WEB_URL || 'http://support-e2e.localhost:4321'
const adminURL = process.env.SUPPORT_E2E_ADMIN_URL || 'http://admin.localhost:4174'
const sqlLiteral = (value: string) => `'${value.replaceAll("'", "''")}'`
function sql(statement: string) {
  if (process.env.SUPPORT_E2E_ISOLATED !== '1' || !databaseURL) throw new Error('Run pnpm test:live-support from the repository root for an isolated fixture database')
  const target = new URL(databaseURL)
  if (!['127.0.0.1', 'localhost'].includes(target.hostname) || target.port !== '54339' || target.pathname !== '/fidibeki') throw new Error('Support E2E fixtures require the dedicated loopback database on port 54339')
  return execFileSync('psql', [databaseURL, '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'], { input: statement, encoding: 'utf8' }).trim()
}
const hash = (value: string) => createHash('sha256').update(value).digest('hex')

test('staff starts a live conversation, member replies and reconnects through the real proxies', async ({ browser }) => {
  const tenantID = randomUUID(), memberID = randomUUID(), adminID = randomUUID(), projectID = randomUUID()
  const memberToken = randomBytes(32).toString('base64url'), adminToken = randomBytes(32).toString('base64url')
  // Insert fails if the synthetic slug is occupied; cleanup uses only our IDs.
  sql(`INSERT INTO tenants(id,name,slug,plan,status) VALUES('${tenantID}','Support test workspace','support-e2e','growth','active');
    INSERT INTO tenant_members(id,tenant_id,email,name,status,permissions) VALUES('${memberID}','${tenantID}','marta@support-e2e.example','Marta Silva','active',ARRAY['feedback:read','issue:read','project:read','tenant:read']);
    UPDATE tenants SET owner_member_id='${memberID}' WHERE id='${tenantID}';
    INSERT INTO platform_admins(id,email,name,permissions) VALUES('${adminID}','${adminID}@support-e2e.example','Ada',ARRAY['platform:tenant:read','platform:support:read','platform:support:write']);
    INSERT INTO tenant_member_sessions(tenant_id,member_id,refresh_hash) VALUES('${tenantID}','${memberID}','${hash(memberToken)}');
    INSERT INTO platform_admin_sessions(admin_id,refresh_hash) VALUES('${adminID}','${hash(adminToken)}');
    INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains) VALUES('${projectID}','${tenantID}','Fidibeki support fixture','pk_live_SUPPORTAAAAAAAAAAAAAAA',ARRAY['support-e2e.localhost']);
    INSERT INTO widget_rules(project_id,name,matcher_type,trigger_type,trigger_delay_seconds,greeting,localizations) VALUES('${projectID}','App feedback','all_pages','delay',0,'How can we improve Fidibeki?','{"en":{"launcher_label":"Feedback","prompt":"How can we improve Fidibeki?","comment_placeholder":"Tell us more","contact_label":"Email","consent_label":"Email me","submit_label":"Send feedback","success_message":"Your feedback was sent."}}');
    INSERT INTO widget_rule_target_conditions(widget_rule_id,kind,matcher_type,position) SELECT id,'include','all_pages',0 FROM widget_rules WHERE project_id='${projectID}';`)
  const memberContext = await browser.newContext()
  const adminContext = await browser.newContext()
  await memberContext.addCookies([{ name: 'fidibeki_tenant_refresh', value: memberToken, domain: new URL(webURL).hostname, path: '/', httpOnly: true, sameSite: 'Strict' }])
  await adminContext.addCookies([{ name: 'fidibeki_platform_refresh', value: adminToken, domain: new URL(adminURL).hostname, path: '/admin/auth', httpOnly: true, sameSite: 'Strict' }])
  const member = await memberContext.newPage(), admin = await adminContext.newPage()
  const browserErrors: string[] = []
  member.on('pageerror', error => browserErrors.push(error.message)); admin.on('pageerror', error => browserErrors.push(error.message))
  try {
    await member.goto(`${webURL}/profile`)
    await expect(member.getByRole('button', { name: 'Messages & feedback' })).toBeVisible()
    await member.getByRole('button', { name: 'Messages & feedback' }).click()
    await expect(member.getByText('Your conversations live here')).toBeVisible()
    await expect(member.getByRole('textbox', { name: 'Your reply' })).toHaveCount(0)
    await member.getByRole('button', { name: 'Close support', exact: true }).click()

    await admin.goto(`${adminURL}/support`)
    await expect(admin.getByRole('status')).toHaveText('Live')
    await admin.getByRole('button', { name: /Marta Silva.*Support test workspace/ }).click()
    await admin.getByRole('textbox', { name: 'Message to Marta Silva' }).fill('Hi Marta, how did your first widget setup go?')
    await admin.getByRole('button', { name: 'Send message', exact: true }).click()
    await expect(member.getByRole('complementary', { name: 'New message from Fidibeki' })).toContainText('Hi Marta, how did your first widget setup go?')
    await member.getByRole('button', { name: /Ada · Fidibeki/ }).click()
    await member.getByRole('textbox', { name: 'Your reply' }).fill('Setup went well. I would love clearer targeting options.')
    await member.getByRole('button', { name: 'Send reply', exact: true }).click()
    await expect(admin.getByRole('log')).toContainText('Setup went well. I would love clearer targeting options.')

    await member.reload()
    await member.getByRole('button', { name: 'Messages & feedback' }).click()
    await expect(member.getByRole('log')).toContainText('Setup went well.')
    const secondTab = await memberContext.newPage()
    await secondTab.goto(`${webURL}/projects`)
    await expect(secondTab.getByRole('button', { name: 'Messages & feedback' })).toBeVisible()
    await admin.getByRole('button', { name: /^Online/ }).click()
    await expect(admin.locator('.support-person').filter({ hasText: 'Marta Silva' })).toHaveCount(1)

    await memberContext.setOffline(true)
    await admin.getByRole('textbox', { name: 'Message to Marta Silva' }).fill('Thanks, I have noted your suggestion.')
    await admin.getByRole('button', { name: 'Send message', exact: true }).click()
    await memberContext.setOffline(false)
    await expect(member.getByRole('log')).toContainText('Thanks, I have noted your suggestion.', { timeout: 30_000 })
    await expect(secondTab.getByRole('complementary', { name: 'New message from Fidibeki' })).toBeHidden()

    const downloadPromise = member.waitForEvent('download')
    await member.getByRole('button', { name: 'Download conversation', exact: true }).click()
    const download = await downloadPromise
    const transcript = JSON.parse(readFileSync((await download.path())!, 'utf8'))
    expect(transcript.messages).toHaveLength(3)
    expect(transcript.messages[1].body).toContain('Setup went well.')

    await member.getByRole('button', { name: 'Give feedback', exact: true }).click()
    await expect(member.locator('#fidibeki-support').getByText('How can we improve Fidibeki?')).toBeVisible()
    await expect(member.locator('[data-launcher]')).toHaveCount(0)
    await member.locator('#fidibeki-support').getByRole('textbox', { name: 'Feedback', exact: true }).fill('The live conversation was helpful.')
    await member.locator('#fidibeki-support').getByRole('button', { name: 'Send feedback', exact: true }).click()
    await expect(member.locator('#fidibeki-support').getByText('Your feedback was sent.')).toBeVisible()
    expect(sql(`SELECT count(*) FROM feedbacks WHERE tenant_id=${sqlLiteral(tenantID)} AND description='The live conversation was helpful.'`)).toBe('1')
    await member.getByRole('button', { name: 'Messages', exact: true }).click()
    await member.keyboard.press('Escape')
    await expect(member.getByRole('button', { name: 'Messages & feedback' })).toBeFocused()
    await member.getByRole('button', { name: 'Messages & feedback' }).click()

    await member.screenshot({ path: '../docs/evidence/live-support-app-desktop.png', animations: 'disabled' })
    await admin.screenshot({ path: '../docs/evidence/live-support-backoffice-desktop.png', animations: 'disabled' })
    await admin.setViewportSize({ width: 390, height: 844 })
    await member.setViewportSize({ width: 390, height: 844 })
    await expect(admin.getByRole('button', { name: 'Back to people' })).toBeVisible()
    for (const page of [member, admin]) expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await member.screenshot({ path: '../docs/evidence/live-support-app-mobile.png', animations: 'disabled' })
    await admin.screenshot({ path: '../docs/evidence/live-support-backoffice-mobile.png', animations: 'disabled' })
    expect(browserErrors).toEqual([])
    expect(sql(`SELECT count(*) FROM support_messages WHERE tenant_id=${sqlLiteral(tenantID)}`)).toBe('3')
  } finally {
    await memberContext.close(); await adminContext.close()
    sql(`DELETE FROM tenants WHERE id='${tenantID}'; DELETE FROM platform_admins WHERE id='${adminID}';`)
  }
})
