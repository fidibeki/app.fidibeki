import { mountReplayConsentPrompt } from '../../src/replay-consent-prompt'
import type { ReplayConsentConfig } from '../../src/types'

const consent: ReplayConsentConfig = {
  mode: 'fidibeki_managed',
  notice_version: '2026-07',
  controller_name: 'Acme',
  privacy_policy_url: 'https://example.com/privacy',
  supported_locales: ['pt-pt'],
  retention_days: 30,
  expiry_days: 30,
  notices: {
    'pt-pt': {
      title: 'Reprodução da sessão',
      description: 'Ajude-nos a melhorar esta experiência.',
      accept_label: 'Aceitar',
      reject_label: 'Recusar',
      withdraw_label: 'Retirar consentimento',
    },
  },
}

describe('managed replay consent prompt', () => {
  it('presents equivalent accept and reject actions without starting capture', async () => {
    const accept = vi.fn(async () => {})
    const reject = vi.fn(async () => {})
    const unmount = mountReplayConsentPrompt({
      consent,
      locale: 'pt-pt',
      state: 'new',
      onAccept: accept,
      onReject: reject,
      onWithdraw: vi.fn(),
    })
    const host = document.querySelector<HTMLElement>('[data-fidibeki-replay-consent]')
    const root = host?.shadowRoot
    const dialog = root?.querySelector<HTMLElement>('[role="dialog"]')
    const buttons = [...(root?.querySelectorAll<HTMLButtonElement>('.decision') ?? [])]

    expect(dialog?.getAttribute('aria-labelledby')).toBe('replay-consent-title')
    expect(root?.textContent).toContain('Reprodução da sessão')
    expect(root?.textContent).toContain('Acme')
    expect(buttons.map(button => button.textContent)).toEqual(['Recusar', 'Aceitar'])
    expect(getComputedStyle(root!.querySelector('.actions')!).gridTemplateColumns).toBe('1fr 1fr')

    buttons[0]?.click()
    await Promise.resolve()
    expect(reject).toHaveBeenCalledTimes(1)
    expect(accept).not.toHaveBeenCalled()
    unmount()
    expect(document.querySelector('[data-fidibeki-replay-consent]')).toBeNull()
  })

  it('keeps withdrawal one action away from the compact privacy control', async () => {
    const withdraw = vi.fn(async () => {})
    const unmount = mountReplayConsentPrompt({
      consent,
      locale: 'pt-pt',
      state: 'active',
      onAccept: vi.fn(),
      onReject: vi.fn(),
      onWithdraw: withdraw,
    })
    const root = document.querySelector<HTMLElement>('[data-fidibeki-replay-consent]')!.shadowRoot!
    const launcher = root.querySelector<HTMLButtonElement>('.privacy-launcher')!
    expect(launcher.textContent).toContain('Privacidade da reprodução')

    launcher.click()
    const withdrawButton = root.querySelector<HTMLButtonElement>('.withdraw')!
    expect(withdrawButton.textContent).toContain('Retirar consentimento')
    withdrawButton.click()
    await Promise.resolve()
    expect(withdraw).toHaveBeenCalledTimes(1)
    unmount()
  })
})
