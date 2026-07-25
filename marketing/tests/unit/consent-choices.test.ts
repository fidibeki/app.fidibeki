import { describe, expect, it } from 'vitest'
import {
  allDenied,
  allGranted,
  normalizeSelections,
  parseStoredConsent,
  serializeConsent,
} from '~/utils/consent-choices'

describe('normalizeSelections', () => {
  it('drops widget purposes when the widget itself is not selected', () => {
    expect(normalizeSelections({
      umami: true,
      widget: false,
      widgetAnalytics: true,
      sessionReplay: true,
    })).toEqual({ umami: true, widget: false, widgetAnalytics: false, sessionReplay: false })
  })

  it('keeps widget purposes when the widget is selected', () => {
    expect(normalizeSelections(allGranted)).toEqual(allGranted)
  })
})

describe('parseStoredConsent', () => {
  it('maps the legacy granted/denied strings to everything/nothing', () => {
    expect(parseStoredConsent('granted')).toEqual(allGranted)
    expect(parseStoredConsent('denied')).toEqual(allDenied)
  })

  it('round-trips granular selections', () => {
    const selections = { umami: false, widget: true, widgetAnalytics: true, sessionReplay: false }
    expect(parseStoredConsent(serializeConsent(selections))).toEqual(selections)
  })

  it('normalizes stored selections that violate the widget dependency', () => {
    expect(parseStoredConsent(JSON.stringify({
      version: 1,
      umami: false,
      widget: false,
      widgetAnalytics: true,
      sessionReplay: true,
    }))).toEqual(allDenied)
  })

  it('treats missing, malformed, or unversioned values as undecided', () => {
    expect(parseStoredConsent(null)).toBeNull()
    expect(parseStoredConsent('')).toBeNull()
    expect(parseStoredConsent('nonsense')).toBeNull()
    expect(parseStoredConsent('{"umami":true}')).toBeNull()
    expect(parseStoredConsent(JSON.stringify({
      version: 2,
      umami: true,
      widget: true,
      widgetAnalytics: true,
      sessionReplay: true,
    }))).toBeNull()
  })
})
