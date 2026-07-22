import {
  cacheIsFresh,
  fetchConfiguration,
  matchRule,
  reportConnection,
  type ConfigCache,
  type TargetingRegexModule,
} from './config'
import {
  createAnalyticsTracker,
  reportFunnelEvents,
  type FunnelEventType,
} from './analytics'
import { getSessionID, isSuppressed, recordWidgetState } from './suppression'
import { armTrigger, type TriggerCleanup } from './triggers'
import type { WidgetConfiguration, WidgetRule } from './types'

type WidgetUnmount = () => void

interface WidgetModule {
  mountWidget(options: {
    configuration: WidgetConfiguration
    rule: WidgetRule
    publicKey: string
    apiBase: URL
    assetBase: URL
    replaySessionID: string | null
    onVisible(): void
    onInteraction(): void
    submissionAnalytics(): import('./analytics').FunnelEvent | null
    onSubmitted(): void
    onDismissed(): void
  }): WidgetUnmount
}

interface ReplayCapture {
	readonly sessionID: string
	stop(): Promise<void>
}

interface ReplayModule {
	startReplayCapture(options: {
		rule: WidgetRule
		publicKey: string
		apiBase: URL
		onError(error: unknown): void
	}): Promise<ReplayCapture | null>
}

interface ErrorModule {
  captureWidgetError(error: unknown, options: { dsn: string; context: Record<string, unknown> }): void
}

const script = findLoaderScript()
const publicKey = script?.dataset.fidibekiKey?.trim() ?? ''

if (script && publicKey) {
  void bootstrap(script, publicKey)
}

async function bootstrap(loaderScript: HTMLScriptElement, key: string): Promise<void> {
  const assetBase = new URL('.', loaderScript.src)
  const configurationEndpoint = new URL('widget/config', assetBase)
  const connectionEndpoint = new URL('widget/connected', assetBase)
  const analyticsEndpoint = new URL('widget/events', assetBase)
  let cache: ConfigCache | null = null
  let cleanupTrigger: TriggerCleanup | null = null
  let unmountWidget: WidgetUnmount | null = null
	let replayCapture: ReplayCapture | null = null
	let replayFingerprint = ''
	let replayTransition = 0
  let evaluation = 0
  const widgetSessionID = currentSessionID()
  const analytics = createAnalyticsTracker({ storage: localStorageIfAvailable() })
  let analyticsDelivery = Promise.resolve()

  const report = async (error: unknown, phase: string) => {
    const dsn = cache?.value.error_reporting_dsn
    if (!dsn) return
    try {
      const module = (await import(new URL('error-reporting.js', assetBase).href)) as ErrorModule
      module.captureWidgetError(error, { dsn, context: { phase, project_id: cache?.value.project_id } })
    } catch {
      // Error reporting must never interfere with the host page.
    }
  }

  const loadConfiguration = async (force: boolean): Promise<WidgetConfiguration> => {
    const now = Date.now()
    if (!force && cacheIsFresh(cache, now)) return cache.value
    const value = await fetchConfiguration({
      endpoint: configurationEndpoint,
      publicKey: key,
    })
    cache = { value, fetchedAt: now }
    return value
  }

  const recordAnalytics = (eventType: FunnelEventType, rule: WidgetRule) => {
    const event = analytics.record(eventType, rule.id)
    if (!event) return
    analyticsDelivery = analyticsDelivery
      .then(() => reportFunnelEvents({ endpoint: analyticsEndpoint, publicKey: key, events: [event] }))
      .catch((error: unknown) => {
        void report(error, 'analytics')
      })
  }

  const evaluate = async (forceRefresh = false) => {
    const currentEvaluation = ++evaluation
    const evaluationURL = window.location.href
    analytics.visit(evaluationURL)
    cleanupTrigger?.()
    cleanupTrigger = null
    unmountWidget?.()
    unmountWidget = null
    try {
      const configuration = await loadConfiguration(forceRefresh)
      if (currentEvaluation !== evaluation) return
      const rule = await matchRule(configuration.rules, evaluationURL, {
        loadRegex: () => import(new URL('targeting-regex.js', assetBase).href) as Promise<TargetingRegexModule>,
        onError: (error) => void report(error, 'targeting'),
      })
			if (currentEvaluation !== evaluation) return
			if (rule) {
				recordAnalytics('matched', rule)
				void reportConnection({ endpoint: connectionEndpoint, publicKey: key, ruleID: rule.id }).catch(() => {})
			}
			await synchronizeReplay(configuration, rule)
			if (currentEvaluation !== evaluation) return
      if (!rule || ruleIsSuppressed(configuration, rule, evaluationURL, widgetSessionID)) return
      cleanupTrigger = armTrigger(rule, () => {
        void import(new URL('widget.js', assetBase).href)
          .then((module) => {
            if (currentEvaluation !== evaluation) return
            unmountWidget = (module as WidgetModule).mountWidget({
              configuration,
              rule,
              publicKey: key,
              apiBase: assetBase,
              assetBase,
					replaySessionID: replayCapture?.sessionID ?? null,
              onVisible: () => recordAnalytics('impression', rule),
              onInteraction: () => recordAnalytics('interaction', rule),
              submissionAnalytics: () => analytics.record('submitted', rule.id),
              onSubmitted: () => persistState(configuration, rule, evaluationURL, widgetSessionID, 'submitted'),
              onDismissed: () => {
					recordAnalytics('dismissed', rule)
					persistState(configuration, rule, evaluationURL, widgetSessionID, 'dismissed')
				},
            })
          })
          .catch((error: unknown) => report(error, 'mount'))
      })
    } catch (error) {
      void report(error, 'configuration')
    }
  }

	const synchronizeReplay = async (configuration: WidgetConfiguration, rule: WidgetRule | null) => {
		const enabled = rule !== null && rule.replay_mode !== 'none' && configuration.features.session_replay
		const fingerprint = enabled ? JSON.stringify({
			id: rule.id,
			mode: rule.replay_mode,
			sampling: rule.replay_sampling_rate,
			duration: rule.replay_max_duration_seconds,
			events: rule.replay_max_events,
			maskInputs: rule.replay_mask_all_inputs,
			maskText: rule.replay_mask_text_selector,
		}) : ''
		if (fingerprint === replayFingerprint) return
		const transition = ++replayTransition
		const previous = replayCapture
		replayCapture = null
		replayFingerprint = fingerprint
		if (previous) void previous.stop()
		if (!enabled || !rule) return
		try {
			const module = (await import(new URL('replay.js', assetBase).href)) as ReplayModule
			const capture = await module.startReplayCapture({
				rule,
				publicKey: key,
				apiBase: assetBase,
				onError: (error) => void report(error, 'replay'),
			})
			if (transition !== replayTransition) {
				if (capture) void capture.stop()
				return
			}
			replayCapture = capture
		} catch (error) {
			void report(error, 'replay')
		}
	}

  installNavigationEvents()
  window.addEventListener('fidibeki:navigation', () => void evaluate())
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void evaluate(true)
  })
  window.setInterval(() => void evaluate(true), 5 * 60 * 1000)
  await evaluate()
}

function currentSessionID(): string {
  try {
    return getSessionID(window.sessionStorage)
  } catch {
    return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  }
}

function localStorageIfAvailable(): Storage | undefined {
  try {
    return window.localStorage
  } catch {
    return undefined
  }
}

function ruleIsSuppressed(
  configuration: WidgetConfiguration,
  rule: WidgetRule,
  rawURL: string,
  sessionID: string,
): boolean {
  try {
    return isSuppressed(window.localStorage, {
      projectID: configuration.project_id,
      ruleID: rule.id,
      rawURL,
      sessionID,
      recurrenceMode: rule.recurrence_mode ?? 'cooldown',
      recurrenceDays: rule.recurrence_days ?? 30,
    })
  } catch {
    return false
  }
}

function persistState(
  configuration: WidgetConfiguration,
  rule: WidgetRule,
  rawURL: string,
  sessionID: string,
  state: 'submitted' | 'dismissed',
): void {
  try {
    recordWidgetState(window.localStorage, {
      projectID: configuration.project_id,
      ruleID: rule.id,
      rawURL,
      sessionID,
      state,
    })
  } catch {
    // Storage restrictions must not interfere with feedback collection.
  }
}

function findLoaderScript(): HTMLScriptElement | null {
  if (document.currentScript instanceof HTMLScriptElement) return document.currentScript
  const scripts = Array.from(document.querySelectorAll<HTMLScriptElement>('script[data-fidibeki-key]'))
  return scripts.find((candidate) => candidate.src.includes('/v1/loader.js')) ?? null
}

function installNavigationEvents(): void {
  const marker = '__fidibekiNavigationInstalled__'
  const markedWindow = window as unknown as Window & Record<string, unknown>
  if (markedWindow[marker]) return
  markedWindow[marker] = true
  const dispatch = () => window.dispatchEvent(new Event('fidibeki:navigation'))
  for (const method of ['pushState', 'replaceState'] as const) {
    const original = history[method]
    history[method] = function (...arguments_: Parameters<History[typeof method]>) {
      const result = original.apply(this, arguments_)
      dispatch()
      return result
    }
  }
  window.addEventListener('popstate', dispatch)
}
