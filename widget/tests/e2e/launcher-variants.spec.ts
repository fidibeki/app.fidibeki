import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import type { WidgetConfiguration, WidgetRule } from '../../src/types'

function launcherRule(overrides: Partial<WidgetRule> = {}): WidgetRule {
  return {
    id: 'rule-launcher', name: 'Launcher', priority: 10,
    matcher_type: 'url_prefix', matcher_value: 'http://127.0.0.1:4173/sample',
    trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
    can_rate: false, can_write_text: true, can_record_audio: false, can_record_video: false,
    can_attach_element: false, can_attach_screenshot: false,
    greeting: 'How was checkout?', position: 'bottom-left', theme: 'light',
    accent_color: '#147d64', replay_mode: 'none', replay_sampling_rate: 0,
    replay_max_duration_seconds: 1800, replay_max_events: 50000,
    replay_mask_all_inputs: true, replay_mask_text_selector: null,
    replay_record_canvas: false, replay_canvas_fps: 2,
    replay_consent: {
      mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
      supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
    },
    analytics_consent: {
      mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
      supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
    },
    recurrence_mode: 'cooldown', recurrence_days: 30,
    presentation: 'launcher',
    localizations: {
      en: {
        launcher_label: 'Feedback', prompt: 'How was checkout?',
        comment_placeholder: 'Tell us more', contact_label: 'Email address',
        consent_label: 'Email me updates about this feedback', submit_label: 'Send feedback',
        success_message: 'Your feedback was sent.',
      },
      pt: {
        launcher_label: 'Dê-nos o seu retorno detalhado sobre esta experiência de compra',
        prompt: 'O checkout foi claro?',
        comment_placeholder: 'Conta-nos mais', contact_label: 'Endereço de email',
        consent_label: 'Enviem-me atualizações sobre este feedback', submit_label: 'Enviar feedback',
        success_message: 'O feedback foi enviado.',
      },
    },
    ...overrides,
  }
}

function configurationFor(rule: WidgetRule): WidgetConfiguration {
  return {
    project_id: 'project-launcher',
    refresh_after_seconds: 300,
    features: {
      ai: false, session_replay: false, slack: false, video: false,
      audio: false, dom_capture: false, custom_domain: false,
    },
    rules: [rule],
  }
}

async function mount(page: Page, rule: WidgetRule): Promise<void> {
  await page.route('**/v1/widget/config*', route => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify(configurationFor(rule)),
  }))
  await page.route('**/v1/widget/connected*', route => route.fulfill({ status: 204 }))
  await page.goto('/sample.html')
}

async function hasNoDocumentOverflow(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
}

for (const variant of ['edge_tab', 'floating_button'] as const) {
  test.describe(`${variant} launcher`, () => {
    for (const [position, side] of [['bottom-left', 'left'], ['bottom-right', 'right']] as const) {
      test(`renders on the ${side} edge with a 44px target and no page overflow`, async ({ page }) => {
        await page.setViewportSize({ width: 1280, height: 800 })
        await mount(page, launcherRule({ launcher_variant: variant, position }))
        const launcher = page.getByRole('button', { name: 'Feedback', exact: true })
        await expect(launcher).toBeVisible()
        await expect(launcher).toHaveAttribute('data-launcher-variant', variant)
        const box = (await launcher.boundingBox())!
        const viewport = page.viewportSize()!
        if (variant === 'edge_tab') {
          // edge_tab is flush with the configured viewport edge; floating_button keeps its
          // existing inset geometry, so the flush assertion only applies to edge_tab.
          if (side === 'left') {
            expect(box.x).toBeLessThanOrEqual(1)
          } else {
            expect(box.x + box.width).toBeGreaterThanOrEqual(viewport.width - 1)
          }
        } else if (side === 'left') {
          expect(box.x).toBeGreaterThan(1)
        } else {
          expect(box.x + box.width).toBeLessThan(viewport.width - 1)
        }
        expect(box.width).toBeGreaterThanOrEqual(44)
        expect(box.height).toBeGreaterThanOrEqual(44)
        expect(await hasNoDocumentOverflow(page)).toBe(true)
        if (process.env.FIDIBEKI_VISUAL === '1') {
          await page.screenshot({ path: `/tmp/fidibeki-launcher-${variant}-${side}-desktop.png`, fullPage: true })
        }
      })
    }

    test('activates via keyboard focus and Enter, opening the shared panel', async ({ page }) => {
      await mount(page, launcherRule({ launcher_variant: variant }))
      const launcher = page.getByRole('button', { name: 'Feedback', exact: true })
      await launcher.focus()
      await page.keyboard.press('Enter')
      await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
      await expect(launcher).toBeHidden()
    })

    test('submits through the same panel path regardless of variant', async ({ page }) => {
      const submissions: Array<Record<string, unknown>> = []
      await mount(page, launcherRule({ launcher_variant: variant }))
      await page.route('**/v1/feedback?*', route => {
        submissions.push(route.request().postDataJSON() as Record<string, unknown>)
        return route.fulfill({
          status: 201, contentType: 'application/json', body: '{"feedback_id":"fb_launcher"}',
        })
      })
      await page.getByRole('button', { name: 'Feedback', exact: true }).click()
      await page.getByRole('textbox', { name: 'Feedback' }).fill('Great checkout.')
      await page.getByRole('button', { name: 'Send feedback' }).click()
      await expect(page.getByText('Your feedback was sent.')).toBeVisible()
      expect(submissions).toHaveLength(1)
    })

    test('dismisses without submitting through the shared close control', async ({ page }) => {
      await mount(page, launcherRule({ launcher_variant: variant }))
      await page.getByRole('button', { name: 'Feedback', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'How was checkout?' })).toBeVisible()
      await page.getByRole('button', { name: 'Close feedback' }).click()
      await expect(page.getByRole('heading', { name: 'How was checkout?' })).toHaveCount(0)
    })

    test('wraps a long Portuguese label without clipping or page overflow on mobile', async ({ page }) => {
      await page.setViewportSize({ width: 375, height: 667 })
      await mount(page, launcherRule({ launcher_variant: variant }))
      await page.evaluate(() => { document.documentElement.lang = 'pt' })
      const launcher = page.locator('[data-launcher]')
      await expect(launcher).toBeVisible()
      const box = (await launcher.boundingBox())!
      expect(box.height).toBeGreaterThanOrEqual(44)
      expect(box.width).toBeGreaterThanOrEqual(44)
      expect(await hasNoDocumentOverflow(page)).toBe(true)
      if (process.env.FIDIBEKI_VISUAL === '1') {
        await page.screenshot({ path: `/tmp/fidibeki-launcher-${variant}-pt-mobile.png`, fullPage: true })
      }
    })
  })
}

test('a missing launcher_variant field defaults to edge_tab at runtime', async ({ page }) => {
  await mount(page, launcherRule())
  await expect(page.locator('[data-launcher]')).toHaveAttribute('data-launcher-variant', 'edge_tab')
  await expect(page.locator('[data-launcher]')).toHaveClass(/edge-tab/)
})
