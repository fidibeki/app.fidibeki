import { mountWidget } from '../../src/widget'
import type { PrivacyChoice, WidgetConfiguration } from '../../src/types'
import { rule } from './fixtures'

const configuration: WidgetConfiguration = {
  project_id: 'project-id',
  default_locale: 'en',
  features: {
    ai: false,
    session_replay: false,
    slack: false,
    video: false,
    audio: false,
    dom_capture: false,
    custom_domain: false,
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

function mount(choices: PrivacyChoice[]) {
  let current = choices
  const listeners = new Set<() => void>()
  const destroy = mountWidget({
    configuration,
    rule: rule({ can_rate: false }),
    publicKey: 'pk_live_test',
    apiBase: new URL('https://widget.example/'),
    assetBase: new URL('https://widget.example/'),
    pageURL: 'https://customer.example/checkout',
    replaySessionID: null,
    onVisible: vi.fn(),
    onInteraction: vi.fn(),
    submissionAnalytics: () => null,
    onSubmitted: vi.fn(),
    onDismissed: vi.fn(),
    privacy: {
      choices: () => current,
      subscribe(listener) {
        listeners.add(listener)
        return () => {
          listeners.delete(listener)
        }
      },
    },
  })
  const shadow = document.querySelector<HTMLElement>('#fidibeki-widget-host')!.shadowRoot!
  const set = (next: PrivacyChoice[]) => {
    current = next
    for (const listener of listeners) listener()
  }
  return { destroy, shadow, set, listeners }
}

describe('privacy column', () => {
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('stays out of the panel entirely when nothing is withdrawable', () => {
    const { destroy, shadow } = mount([])
    expect(shadow.querySelector<HTMLElement>('[data-privacy-links]')!.hidden).toBe(true)
    expect(shadow.querySelector<HTMLElement>('[data-privacy-column]')!.hidden).toBe(true)
    expect(shadow.querySelector<HTMLElement>('[data-panel]')!.classList.contains('privacy-open'))
      .toBe(false)
    destroy()
  })

  it('widens the panel into a second column listing every active grant on request', () => {
    const { destroy, shadow } = mount([
      choice(),
      choice({ purpose: 'session_replay', title: 'Session replay', retentionDays: 30 }),
    ])
    const links = shadow.querySelector<HTMLElement>('[data-privacy-links]')!
    const column = shadow.querySelector<HTMLElement>('[data-privacy-column]')!
    const panel = shadow.querySelector<HTMLElement>('[data-panel]')!
    const open = shadow.querySelector<HTMLButtonElement>('[data-privacy-open]')!

    expect(links.hidden).toBe(false)
    expect(open.textContent).toBe('Privacy choices')
    expect(column.hidden).toBe(true)

    open.click()
    expect(column.hidden).toBe(false)
    expect(panel.classList.contains('privacy-open')).toBe(true)
    expect(open.getAttribute('aria-expanded')).toBe('true')
    const entries = [...column.querySelectorAll<HTMLElement>('.privacy-choice')]
    expect(entries.map(entry => entry.dataset.purpose)).toEqual(['widget_analytics', 'session_replay'])
    expect(entries[0]?.textContent).toContain('Acme Studio · Retained for up to 90 days.')
    expect(entries[1]?.textContent).toContain('Retained for up to 30 days.')
    expect(entries[0]?.querySelector('a')?.getAttribute('href')).toBe('https://example.com/privacy')

    destroy()
  })

  it('withdraws one purpose without touching the other, then reports the outcome', async () => {
    const withdrawAnalytics = vi.fn(async () => {})
    const withdrawReplay = vi.fn(async () => {})
    const analytics = choice({ withdraw: withdrawAnalytics })
    const replay = choice({
      purpose: 'session_replay', title: 'Session replay', withdraw: withdrawReplay,
    })
    const { destroy, shadow, set } = mount([analytics, replay])
    shadow.querySelector<HTMLButtonElement>('[data-privacy-open]')!.click()

    const analyticsEntry = shadow.querySelector<HTMLElement>('[data-purpose="widget_analytics"]')!
    analyticsEntry.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()

    expect(withdrawAnalytics).toHaveBeenCalledTimes(1)
    expect(withdrawReplay).not.toHaveBeenCalled()
    // The controller drops the grant before the widget re-reads it, so the caller's list is what
    // decides what remains withdrawable.
    set([replay])
    const column = shadow.querySelector<HTMLElement>('[data-privacy-column]')!
    expect([...column.querySelectorAll<HTMLElement>('.privacy-choice')].map(e => e.dataset.purpose))
      .toEqual(['session_replay'])
    expect(shadow.querySelector('[data-privacy-status]')?.textContent).toBe('Consent withdrawn.')
    destroy()
  })

  it('keeps the emptied column open on a failed withdrawal so the failure is readable', async () => {
    const failing = choice({ withdraw: async () => { throw new Error('offline') } })
    const { destroy, shadow, set } = mount([failing])
    shadow.querySelector<HTMLButtonElement>('[data-privacy-open]')!.click()
    shadow.querySelector<HTMLElement>('.privacy-choice')!.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()
    // The controller revokes locally before it calls the API, so the grant is gone either way.
    set([])

    expect(shadow.querySelector<HTMLElement>('[data-privacy-column]')!.hidden).toBe(false)
    expect(shadow.querySelector('[data-privacy-status]')?.textContent)
      .toBe('Your choice could not be saved. Please try again.')
    expect(shadow.querySelector<HTMLElement>('[data-privacy-links]')!.hidden).toBe(true)
    destroy()
  })

  it('survives the controller dropping the grant before its withdrawal settles', async () => {
    // createAnalyticsConsentController/createReplayConsentController mark the grant revoked and
    // notify listeners before the revoke request settles, so the column must not read the emptied
    // list as "nothing to show" and close over the outcome it is about to report.
    let notify = () => {}
    const withdrawing = choice({
      withdraw: async () => {
        notify()
        await Promise.resolve()
      },
    })
    const { destroy, shadow, set } = mount([withdrawing])
    notify = () => set([])
    shadow.querySelector<HTMLButtonElement>('[data-privacy-open]')!.click()
    const column = shadow.querySelector<HTMLElement>('[data-privacy-column]')!
    shadow.querySelector<HTMLElement>('.privacy-choice')!.querySelector('button')!.click()
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()

    expect(column.hidden).toBe(false)
    expect(shadow.querySelector('[data-privacy-status]')?.textContent).toBe('Consent withdrawn.')
    expect(column.querySelectorAll('.privacy-choice')).toHaveLength(0)
    destroy()
  })

  it('closes the column on Escape and when the last grant disappears on its own', () => {
    const { destroy, shadow, set } = mount([choice()])
    const column = shadow.querySelector<HTMLElement>('[data-privacy-column]')!
    const open = shadow.querySelector<HTMLButtonElement>('[data-privacy-open]')!

    open.click()
    expect(column.hidden).toBe(false)
    column.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(column.hidden).toBe(true)
    expect(shadow.activeElement).toBe(open)

    open.click()
    expect(column.hidden).toBe(false)
    set([])
    expect(column.hidden).toBe(true)
    expect(shadow.querySelector<HTMLElement>('[data-privacy-links]')!.hidden).toBe(true)
    destroy()
  })

  it('unsubscribes from privacy updates when the widget is destroyed', () => {
    const { destroy, listeners } = mount([choice()])
    expect(listeners.size).toBe(1)
    destroy()
    expect(listeners.size).toBe(0)
  })
})
