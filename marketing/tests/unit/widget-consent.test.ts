import { describe, expect, it } from 'vitest'
import {
  grantWidgetConsents,
  type FidibekiNamespace,
  type WidgetConsentAssertion,
} from '~/utils/widget-consent'

const baseOptions = {
  consentId: 'marketing-consent-1',
  locale: 'en',
  analyticsNoticeVersion: '2026-07',
  replayNoticeVersion: '2026-07',
  delay: () => Promise.resolve(),
}

function recordingNamespace(received: WidgetConsentAssertion[]): FidibekiNamespace {
  const api = {
    grantConsent: (assertion: WidgetConsentAssertion) => {
      received.push(assertion)
      return Promise.resolve({})
    },
  }
  return { analytics: api, replay: api }
}

describe('grantWidgetConsents', () => {
  it('grants both purposes as customer_cmp assertions once the widget namespace appears', async () => {
    const received: WidgetConsentAssertion[] = []
    let namespace: FidibekiNamespace | undefined
    let polls = 0
    await grantWidgetConsents({
      ...baseOptions,
      namespace: () => {
        polls++
        if (polls > 4) namespace ??= recordingNamespace(received)
        return namespace
      },
    })
    expect(received.map(assertion => assertion.purpose).sort()).toEqual([
      'session_replay',
      'widget_analytics',
    ])
    for (const assertion of received) {
      expect(assertion).toMatchObject({
        consentId: 'marketing-consent-1',
        noticeVersion: '2026-07',
        locale: 'en',
        source: 'customer_cmp',
      })
    }
  })

  it('retries while no rule is active yet and succeeds when one becomes active', async () => {
    const received: WidgetConsentAssertion[] = []
    let calls = 0
    const namespace: FidibekiNamespace = {
      analytics: {
        grantConsent: (assertion) => {
          calls++
          if (calls < 3) {
            return Promise.reject(new Error('no analytics-enabled rule is active on this page'))
          }
          received.push(assertion)
          return Promise.resolve({})
        },
      },
    }
    await grantWidgetConsents({
      ...baseOptions,
      replayNoticeVersion: '',
      namespace: () => namespace,
    })
    expect(calls).toBe(3)
    expect(received).toHaveLength(1)
  })

  it('reports a refused grant once without blocking the other purpose', async () => {
    const received: WidgetConsentAssertion[] = []
    const failures: Array<{ purpose: string, error: unknown }> = []
    let analyticsCalls = 0
    const namespace: FidibekiNamespace = {
      analytics: {
        grantConsent: () => {
          analyticsCalls++
          return Promise.reject(
            new Error('analytics consent assertion does not match the current rule'),
          )
        },
      },
      replay: {
        grantConsent: (assertion) => {
          received.push(assertion)
          return Promise.resolve({})
        },
      },
    }
    await grantWidgetConsents({
      ...baseOptions,
      namespace: () => namespace,
      onError: (purpose, error) => failures.push({ purpose, error }),
    })
    expect(analyticsCalls).toBe(1)
    expect(failures).toEqual([{ purpose: 'widget_analytics', error: expect.any(Error) }])
    expect(received.map(assertion => assertion.purpose)).toEqual(['session_replay'])
  })

  it('skips purposes without a configured notice version', async () => {
    const received: WidgetConsentAssertion[] = []
    await grantWidgetConsents({
      ...baseOptions,
      analyticsNoticeVersion: '',
      namespace: () => recordingNamespace(received),
    })
    expect(received.map(assertion => assertion.purpose)).toEqual(['session_replay'])
  })

  it('gives up quietly when the widget never becomes available', async () => {
    await expect(grantWidgetConsents({
      ...baseOptions,
      attempts: 3,
      namespace: () => undefined,
    })).resolves.toBeUndefined()
  })
})
