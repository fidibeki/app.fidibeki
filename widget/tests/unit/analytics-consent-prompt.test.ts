import { mountAnalyticsConsentPrompt } from '../../src/analytics-consent-prompt'
import type { AnalyticsConsentConfig } from '../../src/types'

const consent: AnalyticsConsentConfig = {
  mode: 'fidibeki_managed',
  notice_version: '2026-07',
  controller_name: 'Acme',
  privacy_policy_url: 'https://example.com/privacy',
  supported_locales: ['pt-pt'],
  retention_days: 90,
  expiry_days: 30,
  notices: {
    'pt-pt': {
      title: 'Análise do widget',
      description: 'Ajude-nos a compreender como o widget é usado.',
      accept_label: 'Aceitar',
      reject_label: 'Recusar',
      withdraw_label: 'Retirar consentimento',
    },
  },
}

describe('managed analytics consent prompt', () => {
  it('presents equivalent accept and reject actions without starting collection', async () => {
    const accept = vi.fn(async () => {})
    const reject = vi.fn(async () => {})
    const unmount = mountAnalyticsConsentPrompt({
      consent,
      locale: 'pt-pt',
      state: 'new',
      onAccept: accept,
      onReject: reject,
      onWithdraw: vi.fn(),
    })
    const host = document.querySelector<HTMLElement>('[data-fidibeki-analytics-consent]')
    const root = host?.shadowRoot
    const dialog = root?.querySelector<HTMLElement>('[role="dialog"]')
    const buttons = [...(root?.querySelectorAll<HTMLButtonElement>('.decision') ?? [])]

    expect(dialog?.getAttribute('aria-labelledby')).toBe('analytics-consent-title')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(root?.textContent).toContain('Análise do widget')
    expect(root?.textContent).toContain('Acme')
    expect(buttons.map(button => button.textContent)).toEqual(['Recusar', 'Aceitar'])

    buttons[0]?.click()
    await Promise.resolve()
    expect(reject).toHaveBeenCalledTimes(1)
    expect(accept).not.toHaveBeenCalled()
    unmount()
    expect(document.querySelector('[data-fidibeki-analytics-consent]')).toBeNull()
  })

  it('keeps withdrawal one action away from the compact privacy control', async () => {
    const withdraw = vi.fn(async () => {})
    const unmount = mountAnalyticsConsentPrompt({
      consent,
      locale: 'pt-pt',
      state: 'active',
      onAccept: vi.fn(),
      onReject: vi.fn(),
      onWithdraw: withdraw,
    })
    const root = document.querySelector<HTMLElement>('[data-fidibeki-analytics-consent]')!.shadowRoot!
    const launcher = root.querySelector<HTMLButtonElement>('.privacy-launcher')!
    expect(launcher.textContent).toContain('Privacidade de análise')

    launcher.click()
    const withdrawButton = root.querySelector<HTMLButtonElement>('.withdraw')!
    expect(withdrawButton.textContent).toContain('Retirar consentimento')
    withdrawButton.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(root.querySelector<HTMLElement>('.panel')?.hidden).toBe(true)
    expect(root.activeElement).toBe(launcher)

    launcher.click()
    withdrawButton.click()
    await Promise.resolve()
    expect(withdraw).toHaveBeenCalledTimes(1)
    unmount()
  })

  it('does not render the same dataset host used by the replay consent prompt', () => {
    const unmount = mountAnalyticsConsentPrompt({
      consent, locale: 'pt-pt', state: 'new',
      onAccept: vi.fn(), onReject: vi.fn(), onWithdraw: vi.fn(),
    })
    expect(document.querySelector('[data-fidibeki-replay-consent]')).toBeNull()
    expect(document.querySelector('[data-fidibeki-analytics-consent]')).not.toBeNull()
    unmount()
  })
})
