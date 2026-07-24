import { mountPrivacyControls } from '../../src/privacy-controls'
import type { PrivacyChoice, WidgetConfiguration, WidgetRule } from '../../src/types'
import { rule } from './fixtures'

const configuration: WidgetConfiguration = {
  project_id: 'project-id',
  default_locale: 'en',
  features: {
    ai: false, session_replay: false, slack: false, video: false,
    audio: false, dom_capture: false, custom_domain: false,
  },
  rules: [],
  refresh_after_seconds: 300,
}

function choice(overrides: Partial<PrivacyChoice> = {}): PrivacyChoice {
  return {
    purpose: 'widget_analytics',
    title: 'Widget analytics',
    description: 'Help us understand how the widget is used.',
    controllerName: 'Acme Studio',
    retentionDays: 90,
    privacyPolicyURL: 'https://example.com/privacy',
    withdrawLabel: 'Withdraw consent',
    withdraw: async () => {},
    ...overrides,
  }
}

function mount(choices: PrivacyChoice[], overrides: Partial<WidgetRule> = {}) {
  let current = choices
  const listeners = new Set<() => void>()
  const destroy = mountPrivacyControls({
    rule: rule(overrides),
    configuration,
    choices: () => current,
    subscribe(listener) {
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
      }
    },
  })
  const shadow = document.querySelector<HTMLElement>('[data-fidibeki-privacy-controls]')!.shadowRoot!
  const set = (next: PrivacyChoice[]) => {
    current = next
    for (const listener of listeners) listener()
  }
  return { destroy, shadow, set, listeners }
}

describe('standing privacy control', () => {
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('borrows the rule launcher variant only where the feedback widget has a launcher', () => {
    const cases = [
      { overrides: {}, expected: 'edge_tab' },
      { overrides: { presentation: 'prompt' as const, launcher_variant: 'floating_button' as const }, expected: 'edge_tab' },
      { overrides: { presentation: 'reaction' as const, launcher_variant: 'floating_button' as const }, expected: 'edge_tab' },
      { overrides: { presentation: 'launcher' as const, launcher_variant: 'floating_button' as const }, expected: 'floating_button' },
      { overrides: { presentation: 'launcher' as const, launcher_variant: 'edge_tab' as const }, expected: 'edge_tab' },
    ]
    for (const { overrides, expected } of cases) {
      const { destroy, shadow } = mount([choice()], overrides)
      const launcher = shadow.querySelector<HTMLButtonElement>('[data-privacy-launcher]')!
      expect(launcher.getAttribute('data-launcher-variant')).toBe(expected)
      expect(launcher.classList.contains(expected === 'edge_tab' ? 'edge-tab' : 'floating-button')).toBe(true)
      expect(launcher.textContent).toBe('Privacy choices')
      destroy()
      document.body.replaceChildren()
    }
  })

  it('opens a panel listing every active grant and withdraws one of them', async () => {
    const withdraw = vi.fn(async () => {})
    const { destroy, shadow, set } = mount([
      choice({ withdraw }),
      choice({ purpose: 'session_replay', title: 'Session replay', retentionDays: 30 }),
    ])
    const launcher = shadow.querySelector<HTMLButtonElement>('[data-privacy-launcher]')!
    const panel = shadow.querySelector<HTMLElement>('[data-privacy-panel]')!
    expect(panel.hidden).toBe(true)

    launcher.click()
    expect(panel.hidden).toBe(false)
    expect(launcher.hidden).toBe(true)
    const entries = [...panel.querySelectorAll<HTMLElement>('.privacy-choice')]
    expect(entries.map(entry => entry.dataset.purpose)).toEqual(['widget_analytics', 'session_replay'])
    expect(entries[0]?.textContent).toContain('Acme Studio · Retained for up to 90 days.')

    entries[0]!.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()
    expect(withdraw).toHaveBeenCalledTimes(1)
    set([choice({ purpose: 'session_replay', title: 'Session replay' })])
    expect(shadow.querySelector('[data-privacy-status]')?.textContent).toBe('Consent withdrawn.')
    expect(panel.querySelectorAll('.privacy-choice')).toHaveLength(1)
    destroy()
  })

  it('removes itself once the last grant is gone rather than lingering as an empty control', async () => {
    const { shadow, set } = mount([choice()])
    shadow.querySelector<HTMLButtonElement>('[data-privacy-launcher]')!.click()
    shadow.querySelector<HTMLElement>('.privacy-choice')!.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()
    set([])

    // The outcome stays readable until the visitor closes it...
    expect(document.querySelector('[data-fidibeki-privacy-controls]')).not.toBeNull()
    expect(shadow.querySelector('[data-privacy-status]')?.textContent).toBe('Consent withdrawn.')
    shadow.querySelector<HTMLButtonElement>('[data-privacy-close]')!.click()
    set([])
    // ...and then there is nothing left to offer.
    expect(document.querySelector('[data-fidibeki-privacy-controls]')).toBeNull()
  })

  it('survives the controller dropping the grant before its withdrawal settles', async () => {
    // The consent controllers revoke locally and notify listeners before the API call settles, so
    // the control must not remove itself out from under the outcome it is about to report.
    let notify = () => {}
    const { shadow, set } = mount([choice({
      withdraw: async () => {
        notify()
        await Promise.resolve()
      },
    })])
    notify = () => set([])
    shadow.querySelector<HTMLButtonElement>('[data-privacy-launcher]')!.click()
    shadow.querySelector<HTMLElement>('.privacy-choice')!.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()

    expect(document.querySelector('[data-fidibeki-privacy-controls]')).not.toBeNull()
    expect(shadow.querySelector('[data-privacy-status]')?.textContent).toBe('Consent withdrawn.')
  })

  it('closes on Escape and unsubscribes when destroyed', () => {
    const { destroy, shadow, listeners } = mount([choice()])
    const launcher = shadow.querySelector<HTMLButtonElement>('[data-privacy-launcher]')!
    const panel = shadow.querySelector<HTMLElement>('[data-privacy-panel]')!

    launcher.click()
    expect(panel.hidden).toBe(false)
    panel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(panel.hidden).toBe(true)
    expect(launcher.hidden).toBe(false)
    expect(shadow.activeElement).toBe(launcher)

    expect(listeners.size).toBe(1)
    destroy()
    expect(listeners.size).toBe(0)
    expect(document.querySelector('[data-fidibeki-privacy-controls]')).toBeNull()
  })
})
