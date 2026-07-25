export interface ConsentSelections {
  /** Cookieless Umami page analytics. */
  umami: boolean
  /** Loading the Fidibeki feedback widget at all. */
  widget: boolean
  /** The widget's own usage analytics (widget_analytics grant). */
  widgetAnalytics: boolean
  /** The widget's session replay (session_replay grant). */
  sessionReplay: boolean
}

export const allGranted: ConsentSelections = {
  umami: true,
  widget: true,
  widgetAnalytics: true,
  sessionReplay: true,
}

export const allDenied: ConsentSelections = {
  umami: false,
  widget: false,
  widgetAnalytics: false,
  sessionReplay: false,
}

const storageVersion = 1

interface StoredConsent extends ConsentSelections {
  version: typeof storageVersion
}

// The widget purposes are grants forwarded to the widget; without the widget on the page there is
// nothing to grant, so they always follow the widget selection down.
export function normalizeSelections(selections: ConsentSelections): ConsentSelections {
  return {
    umami: selections.umami,
    widget: selections.widget,
    widgetAnalytics: selections.widget && selections.widgetAnalytics,
    sessionReplay: selections.widget && selections.sessionReplay,
  }
}

/**
 * Reads a stored decision. The pre-granular banner stored the bare strings 'granted'/'denied',
 * which map to everything/nothing; anything unrecognized counts as undecided so the banner simply
 * asks again.
 */
export function parseStoredConsent(raw: string | null): ConsentSelections | null {
  if (raw === 'granted') return allGranted
  if (raw === 'denied') return allDenied
  if (!raw) return null
  try {
    const stored = JSON.parse(raw) as Partial<StoredConsent>
    if (
      stored.version !== storageVersion ||
      typeof stored.umami !== 'boolean' ||
      typeof stored.widget !== 'boolean' ||
      typeof stored.widgetAnalytics !== 'boolean' ||
      typeof stored.sessionReplay !== 'boolean'
    ) {
      return null
    }
    return normalizeSelections({
      umami: stored.umami,
      widget: stored.widget,
      widgetAnalytics: stored.widgetAnalytics,
      sessionReplay: stored.sessionReplay,
    })
  } catch {
    return null
  }
}

export function serializeConsent(selections: ConsentSelections): string {
  return JSON.stringify({ version: storageVersion, ...normalizeSelections(selections) })
}
