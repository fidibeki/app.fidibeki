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
      onAccept: accept,
      onReject: reject,
    })
    const host = document.querySelector<HTMLElement>('[data-fidibeki-replay-consent]')
    const root = host?.shadowRoot
    const dialog = root?.querySelector<HTMLElement>('[role="dialog"]')
    const buttons = [...(root?.querySelectorAll<HTMLButtonElement>('.decision') ?? [])]

    expect(dialog?.getAttribute('aria-labelledby')).toBe('replay-consent-title')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
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

  it('offers the decision only, leaving no standing control over the host page', () => {
    const unmount = mountReplayConsentPrompt({
      consent,
      locale: 'pt-pt',
      onAccept: vi.fn(),
      onReject: vi.fn(),
    })
    const root = document.querySelector<HTMLElement>('[data-fidibeki-replay-consent]')!.shadowRoot!

    // Withdrawal lives in the feedback widget's privacy column, so this host must never keep a
    // launcher pinned over the customer's layout once a decision exists.
    expect(root.querySelector('.privacy-launcher')).toBeNull()
    expect(root.querySelector('.withdraw')).toBeNull()
    expect([...root.querySelectorAll('button')].map(button => button.className))
      .toEqual(['decision reject', 'decision accept'])
    unmount()
  })
})
