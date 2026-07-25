import { mountConsentPrompt } from '../../src/consent-prompt'
import { mountPrivacyControls } from '../../src/privacy-controls'
import { mountWidget } from '../../src/widget'
import type { WidgetConfiguration } from '../../src/types'
import { rule } from './fixtures'

// Must match the blockSelector passed to rrweb in src/modules/replay.ts. Any Fidibeki
// surface mounted on the customer page has to carry this attribute: unblocked hosts get
// recorded, and the widget's own shadow DOM produces events the batch verifier rejects,
// which kills the whole replay session at finalization.
const blockSelector = '[data-fidibeki-block]'

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

describe('replay recording exclusion', () => {
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('excludes the widget host from replay recording', () => {
    const destroy = mountWidget({
      configuration,
      rule: rule({ presentation: 'launcher', can_rate: false }),
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
    expect(document.querySelector('#fidibeki-widget-host')!.matches(blockSelector)).toBe(true)
    destroy()
  })

  it('excludes the consent prompt host from replay recording', () => {
    const unmount = mountConsentPrompt({
      purposes: [{
        purpose: 'session_replay',
        consent: {
          controller_name: 'Acme',
          privacy_policy_url: 'https://example.com/privacy',
          retention_days: 30,
          notices: {
            en: {
              title: 'Session replay',
              description: 'Help us improve this experience.',
              accept_label: 'Accept',
              reject_label: 'Decline',
              withdraw_label: 'Withdraw consent',
            },
          },
        },
        locale: 'en',
        onAccept: vi.fn(async () => {}),
        onReject: vi.fn(async () => {}),
      }],
    })
    expect(document.querySelector('[data-fidibeki-consent]')!.matches(blockSelector)).toBe(true)
    unmount()
  })

  it('excludes the privacy controls host from replay recording', () => {
    const destroy = mountPrivacyControls({
      rule: rule(),
      configuration,
      choices: () => [],
      subscribe: () => () => {},
    })
    expect(
      document.querySelector('[data-fidibeki-privacy-controls]')!.matches(blockSelector),
    ).toBe(true)
    destroy()
  })
})
