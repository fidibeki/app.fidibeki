export type ConsentChoice = 'granted' | 'denied' | 'pending'

const STORAGE_KEY = 'fidibeki-consent'

function readStoredChoice(): ConsentChoice {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'granted' || stored === 'denied') return stored
  } catch {
    // Storage unavailable — treat as undecided every visit.
  }
  return 'pending'
}

/**
 * Consent-gated analytics. Umami is cookieless and collects no personal
 * data, but we still only load it after an explicit opt-in and remember
 * the visitor's choice either way.
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
  }

  function decline() {
    persist('denied')
  }

  function initialize() {
    if (!import.meta.client) return
    choice.value = readStoredChoice()
    if (choice.value === 'granted') loadAnalytics()
  }

  return { choice, accept, decline, initialize }
}
