import { grantWidgetConsents, type FidibekiNamespace } from '~/utils/widget-consent'

export type ConsentChoice = 'granted' | 'denied' | 'pending'

const STORAGE_KEY = 'fidibeki-consent'
const WIDGET_CONSENT_ID_KEY = 'fidibeki-consent-widget-id'

function readStoredChoice(): ConsentChoice {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'granted' || stored === 'denied') return stored
  } catch {
    // Storage unavailable — treat as undecided every visit.
  }
  return 'pending'
}

// The reference this site records for the visitor's acceptance, passed to the widget as the
// customer_cmp consentId. Stable across visits so repeated grants stay idempotent.
function widgetConsentID(): string {
  try {
    const existing = localStorage.getItem(WIDGET_CONSENT_ID_KEY)
    if (existing) return existing
    const created = crypto.randomUUID()
    localStorage.setItem(WIDGET_CONSENT_ID_KEY, created)
    return created
  } catch {
    return crypto.randomUUID()
  }
}

/**
 * Consent-gated analytics and feedback widget. Umami is cookieless and
 * collects no personal data, and the Fidibeki widget only activates once
 * a visitor opts in, but we still only load either after an explicit
 * opt-in and remember the visitor's choice either way.
 *
 * The banner is also this site's CMP for the widget: one acceptance covers
 * Umami plus the widget's own purposes, forwarded to the widget as
 * customer_cmp grants (see utils/widget-consent.ts) so the widget never
 * shows a second prompt of its own. Declining simply never loads the
 * widget, so there is nothing to deny on its side.
 */
export function useConsent() {
  const choice = useState<ConsentChoice>('consent', () => 'pending')
  const config = useRuntimeConfig()

  function loadAnalytics() {
    if (!import.meta.client) return
    if (!config.public.umamiWebsiteId) return
    if (document.querySelector('script[data-website-id]')) return
    const script = document.createElement('script')
    script.defer = true
    script.src = config.public.umamiScriptUrl
    script.dataset.websiteId = config.public.umamiWebsiteId
    document.head.appendChild(script)
  }

  function loadWidget() {
    if (!import.meta.client) return
    if (!config.public.widgetKey) return
    if (!document.querySelector('script[data-fidibeki-key]')) {
      const script = document.createElement('script')
      script.async = true
      script.src = config.public.widgetScriptUrl
      script.dataset.fidibekiKey = config.public.widgetKey
      document.head.appendChild(script)
    }
    void grantWidgetConsents({
      consentId: widgetConsentID(),
      locale: config.public.widgetConsentLocale,
      analyticsNoticeVersion: config.public.widgetAnalyticsNoticeVersion,
      replayNoticeVersion: config.public.widgetReplayNoticeVersion,
      namespace: () => (window as Window & { Fidibeki?: FidibekiNamespace }).Fidibeki,
      onError: (purpose, error) =>
        console.warn(`[fidibeki] widget ${purpose} consent grant failed`, error),
    })
  }

  function persist(value: 'granted' | 'denied') {
    choice.value = value
    try {
      localStorage.setItem(STORAGE_KEY, value)
    } catch {
      // Non-fatal: the banner will simply reappear next visit.
    }
  }

  function accept() {
    persist('granted')
    loadAnalytics()
    loadWidget()
  }

  function decline() {
    persist('denied')
  }

  function initialize() {
    if (!import.meta.client) return
    choice.value = readStoredChoice()
    if (choice.value === 'granted') {
      loadAnalytics()
      loadWidget()
    }
  }

  return { choice, accept, decline, initialize }
}
