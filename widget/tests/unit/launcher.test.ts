import { mountWidget } from '../../src/widget'
import type { WidgetConfiguration, WidgetRule } from '../../src/types'
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

function mount(overrides: Partial<WidgetRule> = {}) {
  const candidate = rule({ presentation: 'launcher', can_rate: false, ...overrides })
  const onInteraction = vi.fn()
  const onVisible = vi.fn()
  const onDismissed = vi.fn()
  const destroy = mountWidget({
    configuration,
    rule: candidate,
    publicKey: 'pk_live_test',
    apiBase: new URL('https://widget.example/'),
    assetBase: new URL('https://widget.example/'),
    pageURL: 'https://customer.example/checkout',
    replaySessionID: null,
    onVisible,
    onInteraction,
    submissionAnalytics: () => null,
    onSubmitted: vi.fn(),
    onDismissed,
  })
  const shadow = document.querySelector<HTMLElement>('#fidibeki-widget-host')!.shadowRoot!
  return { destroy, shadow, onInteraction, onVisible, onDismissed }
}

describe('launcher variants', () => {
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('defaults to the edge tab variant when launcher_variant is missing', () => {
    const { destroy, shadow } = mount()
    const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')!
    expect(launcher.classList.contains('edge-tab')).toBe(true)
    expect(launcher.classList.contains('floating-button')).toBe(false)
    expect(launcher.getAttribute('data-launcher-variant')).toBe('edge_tab')
    destroy()
  })

  it('renders the floating button variant when explicitly configured', () => {
    const { destroy, shadow } = mount({ launcher_variant: 'floating_button' })
    const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')!
    expect(launcher.classList.contains('floating-button')).toBe(true)
    expect(launcher.classList.contains('edge-tab')).toBe(false)
    expect(launcher.getAttribute('data-launcher-variant')).toBe('floating_button')
    destroy()
  })

  it('renders the localized launcher label for both variants', () => {
    for (const variant of ['edge_tab', 'floating_button'] as const) {
      const { destroy, shadow } = mount({ launcher_variant: variant })
      const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')!
      expect(launcher.textContent).toBe('Feedback')
      destroy()
    }
  })

  it('positions the edge tab flush against the configured side', () => {
    for (const [position, side] of [['bottom-left', 'left'], ['bottom-right', 'right']] as const) {
      const { destroy, shadow } = mount({ position })
      const styleText = shadow.querySelector('style')!.textContent!
      expect(styleText).toContain(`.launcher.edge-tab{top:50%;bottom:auto;${side}:0;`)
      expect(styleText).toContain(`.launcher{bottom:12px;left:auto;right:auto;${side}:12px}`)
      destroy()
    }
  })

  it('provides a visible keyboard focus treatment', () => {
    const { destroy, shadow } = mount()
    const styleText = shadow.querySelector('style')!.textContent!
    expect(styleText).toContain(
      '.launcher:focus,.launcher:focus-visible{outline:3px solid var(--fb-on-accent)',
    )
    destroy()
  })

  it('reflects the accent color in the injected stylesheet for both variants', () => {
    for (const variant of ['edge_tab', 'floating_button'] as const) {
      const { destroy, shadow } = mount({ launcher_variant: variant, accent_color: '#123456' })
      const styleText = shadow.querySelector('style')!.textContent!
      expect(styleText).toContain('--fb-accent:#123456')
      destroy()
    }
  })

  it('opens the shared panel, hides the launcher, and moves focus on activation for both variants', () => {
    for (const variant of ['edge_tab', 'floating_button'] as const) {
      const { destroy, shadow, onInteraction, onVisible } = mount({ launcher_variant: variant })
      const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')!
      const panel = shadow.querySelector<HTMLElement>('[data-panel]')!
      expect(panel.hidden).toBe(true)
      expect(onVisible).toHaveBeenCalledTimes(1)
      launcher.click()
      expect(launcher.hidden).toBe(true)
      expect(panel.hidden).toBe(false)
      expect(shadow.activeElement).not.toBeNull()
      expect(onInteraction).toHaveBeenCalledTimes(1)
      launcher.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      expect(onInteraction).toHaveBeenCalledTimes(1)
      destroy()
    }
  })

  it('minimizes back to the launcher on close instead of dismissing before submission', () => {
    const { destroy, shadow, onDismissed } = mount()
    const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')!
    const panel = shadow.querySelector<HTMLElement>('[data-panel]')!
    launcher.click()
    expect(panel.hidden).toBe(false)
    shadow.querySelector<HTMLButtonElement>('[data-close]')!.click()
    expect(panel.hidden).toBe(true)
    expect(launcher.hidden).toBe(false)
    expect(shadow.activeElement).toBe(launcher)
    expect(onDismissed).not.toHaveBeenCalled()
    expect(document.querySelector('#fidibeki-widget-host')).not.toBeNull()
    launcher.click()
    expect(panel.hidden).toBe(false)
    expect(launcher.hidden).toBe(true)
    destroy()
  })

  it('links back to fidibeki.com from the panel footer', () => {
    const { destroy, shadow } = mount()
    const badge = shadow.querySelector<HTMLElement>('.managed-by')!
    expect(badge.textContent).toBe('Managed by fidibeki.com')
    const link = badge.querySelector<HTMLAnchorElement>('a')!
    expect(link.href).toBe('https://fidibeki.com/?ref=widget')
    expect(link.target).toBe('_blank')
    expect(link.rel).toBe('noopener noreferrer')
    destroy()
  })
})
