import { mountWidget } from '../../src/widget'
import type { WidgetConfiguration } from '../../src/types'
import { rule } from './fixtures'

const configuration: WidgetConfiguration = {
  project_id: 'project-id',
  default_locale: 'en',
  features: {
    ai: true,
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

function mount(aiEnabled: boolean, providerLabel?: string) {
  const candidate = rule({
    can_rate: false,
    ai_processing: aiEnabled
      ? {
          enabled: true,
          provider: providerLabel ? 'openai' : 'google_gemini',
          provider_label: providerLabel,
          input_categories: ['feedback_text'],
          privacy_policy_url: 'https://customer.example/privacy',
          policy_version: '2026-07-24',
        }
      : { enabled: false },
  })
  const destroy = mountWidget({
    configuration,
    rule: candidate,
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
  })
  const shadow = document.querySelector<HTMLElement>('#fidibeki-widget-host')!.shadowRoot!
  return { destroy, shadow }
}

describe('AI processing disclosure', () => {
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('shows the localized provider disclosure and customer privacy notice before submission', () => {
    const { destroy, shadow } = mount(true)
    expect(shadow.querySelector('[data-copy="ai-disclosure"]')?.textContent)
      .toContain('Google Gemini')
    expect(shadow.querySelector('[data-copy="ai-disclosure"]')?.textContent)
      .toContain('Content types: written feedback.')
    const privacy = shadow.querySelector<HTMLAnchorElement>('[data-copy="ai-privacy-link"]')
    expect(privacy?.href).toBe('https://customer.example/privacy')
    expect(privacy?.rel).toContain('noopener')
    destroy()
  })

  it('names the customer provider and renders the label as text', () => {
    const { destroy, shadow } = mount(true, 'OpenAI <em>customer</em>')
    expect(shadow.querySelector('[data-copy="ai-disclosure"]')?.textContent).toContain('OpenAI <em>customer</em>')
    expect(shadow.querySelector('[data-copy="ai-disclosure"] em')).toBeNull()
    destroy()
  })

  it('does not show an AI disclosure for a rule where processing is disabled', () => {
    const { destroy, shadow } = mount(false)
    expect(shadow.querySelector('[data-copy="ai-disclosure"]')).toBeNull()
    destroy()
  })
})
