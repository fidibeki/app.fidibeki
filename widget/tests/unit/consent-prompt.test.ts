import { mountConsentPrompt, type ConsentPromptPurpose, type ConsentPromptPurposeConfig } from '../../src/consent-prompt'

const analyticsConsent: ConsentPromptPurposeConfig = {
  controller_name: 'Acme',
  privacy_policy_url: 'https://example.com/privacy',
  retention_days: 90,
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

const replayConsent: ConsentPromptPurposeConfig = {
  controller_name: 'Acme',
  privacy_policy_url: 'https://example.com/privacy',
  retention_days: 30,
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

function analyticsPurpose(overrides: Partial<ConsentPromptPurpose> = {}): ConsentPromptPurpose {
  return {
    purpose: 'widget_analytics',
    consent: analyticsConsent,
    locale: 'pt-pt',
    state: 'new',
    onAccept: vi.fn(async () => {}),
    onReject: vi.fn(async () => {}),
    onWithdraw: vi.fn(async () => {}),
    ...overrides,
  }
}

function replayPurpose(overrides: Partial<ConsentPromptPurpose> = {}): ConsentPromptPurpose {
  return {
    purpose: 'session_replay',
    consent: replayConsent,
    locale: 'pt-pt',
    state: 'new',
    onAccept: vi.fn(async () => {}),
    onReject: vi.fn(async () => {}),
    onWithdraw: vi.fn(async () => {}),
    ...overrides,
  }
}

const shadowRoot = () => document.querySelector<HTMLElement>('[data-fidibeki-consent]')!.shadowRoot!

describe('single-purpose consent prompt', () => {
  it('presents equivalent accept and reject actions using the notice labels', async () => {
    const purpose = replayPurpose()
    const unmount = mountConsentPrompt({ purposes: [purpose] })
    const root = shadowRoot()
    const dialog = root.querySelector<HTMLElement>('[role="dialog"]')
    const buttons = [...root.querySelectorAll<HTMLButtonElement>('.decision')]

    expect(dialog?.getAttribute('aria-labelledby')).toBe('fidibeki-consent-title')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(root.textContent).toContain('Reprodução da sessão')
    expect(root.textContent).toContain('Acme')
    expect(root.textContent).toContain('As interações gravadas são conservadas até 30 dias.')
    expect(buttons.map(button => button.textContent)).toEqual(['Recusar', 'Aceitar'])
    expect(getComputedStyle(root.querySelector('.actions')!).gridTemplateColumns).toBe('1fr 1fr')

    buttons[0]?.click()
    await Promise.resolve()
    expect(purpose.onReject).toHaveBeenCalledTimes(1)
    expect(purpose.onAccept).not.toHaveBeenCalled()
    unmount()
    expect(document.querySelector('[data-fidibeki-consent]')).toBeNull()
  })

  it('keeps withdrawal one action away from the purpose-specific privacy control', async () => {
    const purpose = analyticsPurpose({ state: 'active' })
    const unmount = mountConsentPrompt({ purposes: [purpose] })
    const root = shadowRoot()
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
    expect(purpose.onWithdraw).toHaveBeenCalledTimes(1)
    unmount()
  })

  it('shows an error and re-enables actions when a decision fails', async () => {
    const purpose = replayPurpose({ onAccept: vi.fn(async () => { throw new Error('offline') }) })
    const unmount = mountConsentPrompt({ purposes: [purpose] })
    const root = shadowRoot()
    root.querySelector<HTMLButtonElement>('.accept')!.click()
    await vi.waitFor(() => {
      expect(root.querySelector('.status')?.textContent).toContain('Não foi possível guardar a escolha.')
    })
    expect(root.querySelector<HTMLButtonElement>('.accept')?.disabled).toBe(false)
    unmount()
  })
})

describe('combined consent prompt', () => {
  it('renders both purposes in one dialog with a shared header and one policy link', () => {
    const unmount = mountConsentPrompt({ purposes: [analyticsPurpose(), replayPurpose()] })
    expect(document.querySelectorAll('[data-fidibeki-consent]')).toHaveLength(1)
    const root = shadowRoot()
    expect(root.querySelector('#fidibeki-consent-title')?.textContent).toBe('Escolhas de privacidade')
    expect(root.textContent).toContain('Análise do widget')
    expect(root.textContent).toContain('Reprodução da sessão')
    expect(root.textContent).toContain('Os dados de análise são conservados até 90 dias.')
    expect(root.textContent).toContain('As interações gravadas são conservadas até 30 dias.')
    expect(root.querySelectorAll('a')).toHaveLength(1)
    const toggles = [...root.querySelectorAll<HTMLInputElement>('.toggle')]
    expect(toggles).toHaveLength(2)
    expect(toggles.every(toggle => !toggle.checked)).toBe(true)
    unmount()
  })

  it('accepts every pending purpose from "accept all"', async () => {
    const analytics = analyticsPurpose()
    const replay = replayPurpose()
    const unmount = mountConsentPrompt({ purposes: [analytics, replay] })
    shadowRoot().querySelector<HTMLButtonElement>('.accept-all')!.click()
    await vi.waitFor(() => {
      expect(analytics.onAccept).toHaveBeenCalledTimes(1)
      expect(replay.onAccept).toHaveBeenCalledTimes(1)
    })
    expect(analytics.onReject).not.toHaveBeenCalled()
    expect(replay.onReject).not.toHaveBeenCalled()
    unmount()
  })

  it('rejects every pending purpose from "reject all"', async () => {
    const analytics = analyticsPurpose()
    const replay = replayPurpose()
    const unmount = mountConsentPrompt({ purposes: [analytics, replay] })
    shadowRoot().querySelector<HTMLButtonElement>('.reject-all')!.click()
    await vi.waitFor(() => {
      expect(analytics.onReject).toHaveBeenCalledTimes(1)
      expect(replay.onReject).toHaveBeenCalledTimes(1)
    })
    expect(analytics.onAccept).not.toHaveBeenCalled()
    expect(replay.onAccept).not.toHaveBeenCalled()
    unmount()
  })

  it('applies the customized toggles from "save choices"', async () => {
    const analytics = analyticsPurpose()
    const replay = replayPurpose()
    const unmount = mountConsentPrompt({ purposes: [analytics, replay] })
    const root = shadowRoot()
    root.querySelector<HTMLInputElement>('.toggle[data-purpose="widget_analytics"]')!.checked = true
    root.querySelector<HTMLButtonElement>('.save')!.click()
    await vi.waitFor(() => {
      expect(analytics.onAccept).toHaveBeenCalledTimes(1)
      expect(replay.onReject).toHaveBeenCalledTimes(1)
    })
    expect(analytics.onReject).not.toHaveBeenCalled()
    expect(replay.onAccept).not.toHaveBeenCalled()
    unmount()
  })

  it('wraps batched decisions in the provided exclusive runner', async () => {
    let batches = 0
    const runExclusive = async <T,>(action: () => Promise<T>): Promise<T> => {
      batches++
      return action()
    }
    const analytics = analyticsPurpose()
    const replay = replayPurpose()
    const unmount = mountConsentPrompt({ purposes: [analytics, replay], runExclusive })
    shadowRoot().querySelector<HTMLButtonElement>('.accept-all')!.click()
    await vi.waitFor(() => expect(analytics.onAccept).toHaveBeenCalledTimes(1))
    expect(batches).toBe(1)
    unmount()
  })

  it('lets an already-granted purpose be withdrawn while another is still pending', async () => {
    const analytics = analyticsPurpose()
    const replay = replayPurpose({ state: 'active' })
    const unmount = mountConsentPrompt({ purposes: [analytics, replay] })
    const root = shadowRoot()
    expect(root.querySelector<HTMLElement>('.panel')?.hidden).toBe(false)
    expect(root.querySelectorAll('.toggle')).toHaveLength(1)
    root.querySelector<HTMLButtonElement>('.withdraw[data-purpose="session_replay"]')!.click()
    await Promise.resolve()
    expect(replay.onWithdraw).toHaveBeenCalledTimes(1)
    unmount()
  })

  it('collapses to a shared privacy launcher when every purpose is already granted', async () => {
    const analytics = analyticsPurpose({ state: 'active' })
    const replay = replayPurpose({ state: 'active' })
    const unmount = mountConsentPrompt({ purposes: [analytics, replay] })
    const root = shadowRoot()
    const launcher = root.querySelector<HTMLButtonElement>('.privacy-launcher')!
    expect(launcher.textContent).toContain('Escolhas de privacidade')
    expect(root.querySelector<HTMLElement>('.panel')?.hidden).toBe(true)

    launcher.click()
    expect(root.querySelector<HTMLElement>('.panel')?.hidden).toBe(false)
    const withdrawals = [...root.querySelectorAll<HTMLButtonElement>('.withdraw')]
    expect(withdrawals).toHaveLength(2)
    withdrawals[0]?.click()
    await Promise.resolve()
    expect(analytics.onWithdraw).toHaveBeenCalledTimes(1)
    expect(replay.onWithdraw).not.toHaveBeenCalled()
    unmount()
  })
})
