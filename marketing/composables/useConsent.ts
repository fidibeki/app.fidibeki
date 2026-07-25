import {
  allDenied,
  allGranted,
  normalizeSelections,
  parseStoredConsent,
  serializeConsent,
  type ConsentSelections,
} from '~/utils/consent-choices'
import { grantWidgetConsents, type FidibekiNamespace } from '~/utils/widget-consent'

const STORAGE_KEY = 'fidibeki-consent'
const WIDGET_CONSENT_ID_KEY = 'fidibeki-consent-widget-id'

function readStoredSelections(): ConsentSelections | null {
  try {
    return parseStoredConsent(localStorage.getItem(STORAGE_KEY))
  } catch {
    // Storage unavailable — treat as undecided every visit.
    return null
  }
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
 * The banner is also this site's CMP for the widget: the visitor can accept
 * everything, decline everything, or pick purposes individually. Selected
 * widget purposes are forwarded to the widget as customer_cmp grants (see
 * utils/widget-consent.ts) so the widget never shows a second prompt of its
 * own; unselected ones are simply never granted, which in customer_cmp mode
 * means the widget collects nothing for them.
 */
export function useConsent() {
  const selections = useState<ConsentSelections | null>('consent', () => null)
  const pending = computed(() => selections.value === null)
  const config = useRuntimeConfig()

  function loadAnalytics() {
    if (!config.public.umamiWebsiteId) return
    if (document.querySelector('script[data-website-id]')) return
    const script = document.createElement('script')
    script.defer = true
    script.src = config.public.umamiScriptUrl
    script.dataset.websiteId = config.public.umamiWebsiteId
    document.head.appendChild(script)
  }

  function loadWidget(granted: ConsentSelections) {
    if (!config.public.widgetKey) return
    if (!document.querySelector('script[data-fidibeki-key]')) {
      const script = document.createElement('script')
      script.async = true
      script.src = config.public.widgetScriptUrl
      script.dataset.fidibekiKey = config.public.widgetKey
      document.head.appendChild(script)
    }
    if (!granted.widgetAnalytics && !granted.sessionReplay) return
    void grantWidgetConsents({
      consentId: widgetConsentID(),
      locale: config.public.widgetConsentLocale,
      analyticsNoticeVersion: granted.widgetAnalytics
        ? config.public.widgetAnalyticsNoticeVersion
        : '',
      replayNoticeVersion: granted.sessionReplay ? config.public.widgetReplayNoticeVersion : '',
      namespace: () => (window as Window & { Fidibeki?: FidibekiNamespace }).Fidibeki,
      onError: (purpose, error) =>
        console.warn(`[fidibeki] widget ${purpose} consent grant failed`, error),
    })
  }

  function apply(granted: ConsentSelections) {
    if (!import.meta.client) return
    if (granted.umami) loadAnalytics()
    if (granted.widget) loadWidget(granted)
  }

  function decide(value: ConsentSelections) {
    const granted = normalizeSelections(value)
    selections.value = granted
    try {
      localStorage.setItem(STORAGE_KEY, serializeConsent(granted))
    } catch {
      // Non-fatal: the banner will simply reappear next visit.
    }
    apply(granted)
  }

  function accept() {
    decide(allGranted)
  }

  function decline() {
    decide(allDenied)
  }

  function save(value: ConsentSelections) {
    decide(value)
  }

  function initialize() {
    if (!import.meta.client) return
    const stored = readStoredSelections()
    selections.value = stored
    if (stored) apply(stored)
  }

  return { pending, accept, decline, save, initialize }
}
