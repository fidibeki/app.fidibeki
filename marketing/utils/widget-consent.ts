export type WidgetConsentPurpose = 'widget_analytics' | 'session_replay'

export interface WidgetConsentAssertion {
  consentId: string
  purpose: WidgetConsentPurpose
  noticeVersion: string
  locale: string
  source: 'customer_cmp'
}

interface WidgetConsentAPI {
  grantConsent(assertion: WidgetConsentAssertion): Promise<unknown>
}

export interface FidibekiNamespace {
  analytics?: WidgetConsentAPI
  replay?: WidgetConsentAPI
}

export interface GrantWidgetConsentsOptions {
  consentId: string
  locale: string
  analyticsNoticeVersion: string
  replayNoticeVersion: string
  namespace(): FidibekiNamespace | undefined
  attempts?: number
  delay?(milliseconds: number): Promise<void>
  onError?(purpose: WidgetConsentPurpose, error: unknown): void
}

const retryDelayMs = 500

/**
 * Forwards the banner's affirmative choice to the feedback widget as customer_cmp grants for
 * widget_analytics and session_replay — the two purposes the widget accepts from a host-page CMP.
 * AI analysis has no host-page grant: its disclosure lives inside the widget's feedback form.
 *
 * The widget loads its configuration and matches a rule asynchronously after the loader script is
 * injected, so grants are retried until a rule is active. A grant the rule refuses (notice-version
 * mismatch, or the rule is not in customer_cmp mode) is a configuration problem retrying cannot
 * fix: it is reported once and dropped, and the widget keeps collecting nothing for that purpose.
 */
export async function grantWidgetConsents(options: GrantWidgetConsentsOptions): Promise<void> {
  const attempts = options.attempts ?? 30
  const delay = options.delay
    ?? (milliseconds => new Promise<void>(resolve => setTimeout(resolve, milliseconds)))
  const pending = new Map<WidgetConsentPurpose, string>()
  if (options.analyticsNoticeVersion) pending.set('widget_analytics', options.analyticsNoticeVersion)
  if (options.replayNoticeVersion) pending.set('session_replay', options.replayNoticeVersion)
  for (let attempt = 0; attempt < attempts && pending.size > 0; attempt++) {
    if (attempt > 0) await delay(retryDelayMs)
    for (const [purpose, noticeVersion] of [...pending]) {
      const namespace = options.namespace()
      const api = purpose === 'widget_analytics' ? namespace?.analytics : namespace?.replay
      if (!api) continue
      try {
        await api.grantConsent({
          consentId: options.consentId,
          purpose,
          noticeVersion,
          locale: options.locale,
          source: 'customer_cmp',
        })
        pending.delete(purpose)
      } catch (error) {
        if (isRetryable(error)) continue
        pending.delete(purpose)
        options.onError?.(purpose, error)
      }
    }
  }
}

// "no analytics-enabled rule is active on this page" / "no replay-enabled rule is active on this
// page": the widget has not finished matching a rule yet. Anything else will not be fixed by
// asking again immediately; the next page view with a stored acceptance retries from scratch.
function isRetryable(error: unknown): boolean {
  return error instanceof Error && error.message.includes('rule is active on this page')
}
